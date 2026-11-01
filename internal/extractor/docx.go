package extractor

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

// maxDocxXMLBytes bounds the decompressed word/document.xml, so a zip bomb
// cannot exhaust memory before the rune limit is checked.
const maxDocxXMLBytes = 64 << 20

// docxToText turns a .docx into plain text: headings become Markdown "#"
// lines, paragraphs become lines, table rows become " | "-joined lines.
// Images, text boxes' layout, headers, footers and comments are dropped.
func docxToText(data []byte) (string, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", ErrInvalidDocx
	}
	var document *zip.File
	for _, file := range archive.File {
		if file.Name == "word/document.xml" {
			document = file
			break
		}
	}
	if document == nil {
		return "", ErrInvalidDocx
	}
	if document.UncompressedSize64 > maxDocxXMLBytes {
		return "", ErrTooLong
	}
	reader, err := document.Open()
	if err != nil {
		return "", ErrInvalidDocx
	}
	defer reader.Close()

	decoder := xml.NewDecoder(io.LimitReader(reader, maxDocxXMLBytes+1))
	var (
		out        strings.Builder
		para       strings.Builder
		cell       strings.Builder
		cells      []string
		heading    int
		inText     bool
		tableDepth int
	)
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", ErrInvalidDocx
		}
		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "Fallback", "tabs":
				// mc:Fallback repeats the mc:Choice content for old readers,
				// and w:tabs only defines tab stops (its w:tab children are
				// not tab characters); reading either would duplicate text.
				if err := decoder.Skip(); err != nil {
					return "", ErrInvalidDocx
				}
			case "p":
				para.Reset()
				heading = 0
			case "pStyle":
				heading = headingLevel(attrValue(t, "val"))
			case "t":
				inText = true
			case "tab":
				para.WriteByte('\t')
			case "br", "cr":
				para.WriteByte('\n')
			case "tbl":
				tableDepth++
			}
		case xml.CharData:
			if inText {
				para.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				line := strings.TrimRight(para.String(), " \t\n")
				para.Reset()
				if strings.TrimSpace(line) == "" {
					continue
				}
				if tableDepth > 0 {
					if cell.Len() > 0 {
						cell.WriteByte(' ')
					}
					cell.WriteString(strings.ReplaceAll(line, "\n", " "))
					continue
				}
				if heading > 0 {
					out.WriteString(strings.Repeat("#", heading))
					out.WriteByte(' ')
				}
				out.WriteString(line)
				out.WriteByte('\n')
			case "tc":
				cells = append(cells, strings.TrimSpace(cell.String()))
				cell.Reset()
			case "tr":
				if len(cells) > 0 {
					out.WriteString(strings.Join(cells, " | "))
					out.WriteByte('\n')
				}
				cells = nil
			case "tbl":
				if tableDepth > 0 {
					tableDepth--
				}
			}
		}
	}
	return out.String(), nil
}

func attrValue(element xml.StartElement, name string) string {
	for _, attr := range element.Attr {
		if attr.Name.Local == name {
			return attr.Value
		}
	}
	return ""
}

// headingLevel maps Word style ids ("Heading1", "heading 2", "Title") to a
// Markdown heading level, 0 for body text.
func headingLevel(style string) int {
	style = strings.ToLower(strings.ReplaceAll(style, " ", ""))
	if style == "title" {
		return 1
	}
	if strings.HasPrefix(style, "heading") && len(style) == len("heading")+1 {
		if level := int(style[len(style)-1] - '0'); level >= 1 && level <= 6 {
			return level
		}
	}
	return 0
}

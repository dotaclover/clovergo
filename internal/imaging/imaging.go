// Package imaging turns an image prompt into a local card picture. No image
// model is called: the prompt is laid out as an SVG and rasterized by whatever
// SVG converter this machine has. When nothing can rasterize SVG the caller is
// told, so the draft can drop its images instead of pretending they exist.
package imaging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrNoRasterizer means no local tool can turn SVG into PNG.
var ErrNoRasterizer = errors.New("no local SVG-to-PNG converter is installed (ImageMagick, librsvg, Inkscape, or ffmpeg with an SVG decoder)")

const (
	cardWidth  = 1080
	cardHeight = 1440
)

type converterKind int

const (
	converterMagick converterKind = iota
	converterFFmpeg
	converterRSVG
	converterInkscape
)

type converter struct {
	kind converterKind
	path string
}

var (
	detectOnce sync.Once
	detected   *converter
	detectErr  error
)

// Available reports whether this machine can rasterize an SVG, and names the
// converter in use.
func Available() (bool, string) {
	c, err := rasterizer()
	if err != nil {
		return false, ""
	}
	switch c.kind {
	case converterMagick:
		return true, "ImageMagick"
	case converterFFmpeg:
		return true, "ffmpeg"
	case converterRSVG:
		return true, "rsvg-convert"
	case converterInkscape:
		return true, "Inkscape"
	}
	return true, c.path
}

func rasterizer() (*converter, error) {
	detectOnce.Do(func() {
		detected, detectErr = detect()
	})
	return detected, detectErr
}

// detect looks for converters in the order that works without extra setup on
// Windows: ImageMagick (installed on this class of machine), then librsvg and
// Inkscape, then an ffmpeg build that actually lists an SVG decoder.
func detect() (*converter, error) {
	for _, path := range magickCandidates() {
		return &converter{kind: converterMagick, path: path}, nil
	}
	for _, name := range []string{"rsvg-convert", "rsvg-convert.exe"} {
		if path, err := exec.LookPath(name); err == nil {
			return &converter{kind: converterRSVG, path: path}, nil
		}
	}
	for _, name := range []string{"inkscape", "inkscape.exe"} {
		if path, err := exec.LookPath(name); err == nil {
			return &converter{kind: converterInkscape, path: path}, nil
		}
	}
	if path, err := exec.LookPath("ffmpeg"); err == nil && ffmpegHasSVG(path) {
		return &converter{kind: converterFFmpeg, path: path}, nil
	}
	return nil, ErrNoRasterizer
}

// magickCandidates finds magick.exe even when its install directory is not on
// PATH, which is the normal state of an ImageMagick install on Windows.
func magickCandidates() []string {
	var result []string
	if path, err := exec.LookPath("magick"); err == nil {
		result = append(result, path)
	}
	roots := []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramW6432"),
		`C:\Program Files`,
	}
	seen := map[string]struct{}{}
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(root, "ImageMagick*", "magick.exe"))
		sort.Sort(sort.Reverse(sort.StringSlice(matches)))
		for _, match := range matches {
			if _, duplicate := seen[match]; duplicate {
				continue
			}
			if info, err := os.Stat(match); err == nil && !info.IsDir() {
				seen[match] = struct{}{}
				result = append(result, match)
			}
		}
	}
	return result
}

func ffmpegHasSVG(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "-hide_banner", "-decoders").Output()
	return err == nil && bytes.Contains(out, []byte(" svg "))
}

// Card lays a title out as an SVG card (title only: the prompt is a brief for
// a future image generator, not something to print on the picture). index is
// 1-based; total is the number of pictures in the post, and the "01 / 03"
// marker is only drawn when there is more than one.
func Card(title string, index, total int) []byte {
	title = strings.TrimSpace(title)
	colors := palette(title + fmt.Sprint(index))
	titleLines := wrapRunes(title, 11)
	if len(titleLines) > 4 {
		titleLines = titleLines[:4]
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`,
		cardWidth, cardHeight, cardWidth, cardHeight)
	fmt.Fprintf(&b, `<defs><linearGradient id="bg" x1="0" y1="0" x2="1" y2="1">
<stop offset="0" stop-color="%s"/><stop offset="1" stop-color="%s"/></linearGradient></defs>`,
		colors[0], colors[1])
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="url(#bg)"/>`, cardWidth, cardHeight)
	// Soft shapes keep the card from reading as a plain text slide.
	fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="%d" fill="%s" fill-opacity="0.18"/>`,
		cardWidth-120, 200, 300, colors[2])
	fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="%d" fill="%s" fill-opacity="0.14"/>`,
		140, cardHeight-260, 260, colors[2])
	fmt.Fprintf(&b, `<rect x="80" y="104" width="%d" height="3" fill="%s" fill-opacity="0.65"/>`, cardWidth-160, colors[2])

	if total > 1 {
		fmt.Fprintf(&b, `<text x="80" y="170" font-family="'Microsoft YaHei','PingFang SC',sans-serif" font-size="30" fill="%s" fill-opacity="0.85">%02d / %02d</text>`,
			colors[2], index, total)
	}

	// The title block is centered vertically.
	const lineHeight = 116
	y := (cardHeight-len(titleLines)*lineHeight)/2 + 84
	for _, line := range titleLines {
		fmt.Fprintf(&b, `<text x="80" y="%d" font-family="'Microsoft YaHei','PingFang SC',sans-serif" font-size="84" font-weight="700" fill="#ffffff">%s</text>`,
			y, html.EscapeString(line))
		y += lineHeight
	}
	b.WriteString(`</svg>`)
	return []byte(b.String())
}

// Rasterize turns an SVG into PNG with the detected converter. The files are
// written to a temporary directory and removed before returning.
func Rasterize(svg []byte) ([]byte, error) {
	c, err := rasterizer()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "clovergo-card-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	input := filepath.Join(dir, "card.svg")
	output := filepath.Join(dir, "card.png")
	if err := os.WriteFile(input, svg, 0o644); err != nil {
		return nil, err
	}
	var command *exec.Cmd
	switch c.kind {
	case converterMagick:
		command = exec.Command(c.path, input, output)
	case converterFFmpeg:
		command = exec.Command(c.path, "-hide_banner", "-loglevel", "error", "-y", "-i", input, "-frames:v", "1", output)
	case converterRSVG:
		command = exec.Command(c.path, "-o", output, input)
	case converterInkscape:
		command = exec.Command(c.path, "--export-type=png", "--export-filename="+output, input)
	default:
		return nil, ErrNoRasterizer
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	command.Stdin = nil
	done := make(chan error, 1)
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start SVG converter: %w", err)
	}
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			message := strings.TrimSpace(stderr.String())
			if message == "" {
				message = err.Error()
			}
			return nil, fmt.Errorf("convert SVG to PNG: %s", firstLine(message))
		}
	case <-time.After(45 * time.Second):
		_ = command.Process.Kill()
		return nil, errors.New("convert SVG to PNG: the converter timed out")
	}
	png, err := os.ReadFile(output)
	if err != nil {
		return nil, fmt.Errorf("read converted PNG: %w", err)
	}
	if len(png) == 0 {
		return nil, errors.New("convert SVG to PNG: the converter produced no output")
	}
	return png, nil
}

// Render is Card followed by Rasterize.
func Render(title string, index, total int) ([]byte, error) {
	return Rasterize(Card(title, index, total))
}

func firstLine(value string) string {
	if at := strings.IndexByte(value, '\n'); at >= 0 {
		return value[:at]
	}
	return value
}

// wrapRunes breaks text into lines of at most perLine runes, preferring a
// break after a space or punctuation so a line does not end mid-phrase.
func wrapRunes(text string, perLine int) []string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil
	}
	if perLine < 4 {
		perLine = 4
	}
	var lines []string
	for len(runes) > perLine {
		cut := perLine
		for i := perLine; i > perLine/2; i-- {
			if strings.ContainsRune("，。；：、！？,.;:!? ", runes[i-1]) {
				cut = i
				break
			}
		}
		lines = append(lines, string(runes[:cut]))
		runes = runes[cut:]
		for len(runes) > 0 && runes[0] == ' ' {
			runes = runes[1:]
		}
	}
	if len(runes) > 0 {
		lines = append(lines, string(runes))
	}
	return lines
}

// palette picks a stable pair of gradient colors from the text, so the same
// prompt always produces the same card.
var gradients = [][3]string{
	{"#0f172a", "#334155", "#7dd3fc"},
	{"#1e1b4b", "#4338ca", "#c4b5fd"},
	{"#052e2b", "#0f766e", "#99f6e4"},
	{"#3b0764", "#7e22ce", "#f0abfc"},
	{"#431407", "#c2410c", "#fed7aa"},
	{"#3f1d38", "#be185d", "#fbcfe8"},
}

func palette(seed string) [3]string {
	sum := sha256.Sum256([]byte(seed))
	index := binary.BigEndian.Uint64(sum[:8]) % uint64(len(gradients))
	return gradients[index]
}

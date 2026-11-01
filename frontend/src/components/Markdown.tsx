import { Fragment, type ReactNode } from "react";

// A deliberately small Markdown renderer that only ever produces React text
// nodes. Model output is untrusted, so it is never injected as HTML.

function renderInline(text: string, keyPrefix: string): ReactNode[] {
  const nodes: ReactNode[] = [];
  const pattern = /(\*\*[^*\n]+\*\*|`[^`\n]+`|\*[^*\n]+\*|\[K\d+\])/g;
  let last = 0;
  let match: RegExpExecArray | null;
  let index = 0;
  while ((match = pattern.exec(text)) !== null) {
    if (match.index > last) nodes.push(text.slice(last, match.index));
    const token = match[0];
    const key = `${keyPrefix}-${index++}`;
    if (token.startsWith("[K")) {
      // Agent citation marker, e.g. [K12]: shown as a small chip.
      nodes.push(
        <sup key={key} className="mx-0.5 rounded bg-emerald-50 px-1 font-mono text-[0.7em] text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-300">
          {token.slice(1, -1)}
        </sup>,
      );
    } else if (token.startsWith("**")) {
      nodes.push(
        <strong key={key} className="font-semibold text-zinc-900 dark:text-zinc-50">
          {token.slice(2, -2)}
        </strong>,
      );
    } else if (token.startsWith("`")) {
      nodes.push(
        <code key={key} className="rounded bg-zinc-100 px-1 py-0.5 font-mono text-[0.85em] dark:bg-zinc-800">
          {token.slice(1, -1)}
        </code>,
      );
    } else {
      nodes.push(<em key={key}>{token.slice(1, -1)}</em>);
    }
    last = match.index + token.length;
  }
  if (last < text.length) nodes.push(text.slice(last));
  return nodes;
}

type Block =
  | { type: "heading"; level: number; text: string }
  | { type: "paragraph"; lines: string[] }
  | { type: "list"; ordered: boolean; items: string[] }
  | { type: "quote"; lines: string[] }
  | { type: "code"; text: string }
  | { type: "table"; header: string[]; rows: string[][] }
  | { type: "rule" };

const tableSeparator = /^\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?$/;

function tableCells(line: string): string[] {
  return line.trim().replace(/^\|/, "").replace(/\|$/, "").split("|").map((cell) => cell.trim());
}

function parse(markdown: string): Block[] {
  const lines = markdown.replace(/\r\n?/g, "\n").split("\n");
  const blocks: Block[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    const trimmed = line.trim();
    if (!trimmed) {
      i++;
      continue;
    }
    if (trimmed.startsWith("```")) {
      const body: string[] = [];
      i++;
      while (i < lines.length && !lines[i].trim().startsWith("```")) body.push(lines[i++]);
      i++;
      blocks.push({ type: "code", text: body.join("\n") });
      continue;
    }
    if (trimmed.includes("|") && i + 1 < lines.length && tableSeparator.test(lines[i + 1].trim())) {
      const header = tableCells(trimmed);
      i += 2;
      const rows: string[][] = [];
      while (i < lines.length && lines[i].trim().includes("|")) rows.push(tableCells(lines[i++]));
      blocks.push({ type: "table", header, rows });
      continue;
    }
    const heading = /^(#{1,6})\s+(.*)$/.exec(trimmed);
    if (heading) {
      blocks.push({ type: "heading", level: heading[1].length, text: heading[2] });
      i++;
      continue;
    }
    if (/^(-{3,}|\*{3,}|_{3,})$/.test(trimmed)) {
      blocks.push({ type: "rule" });
      i++;
      continue;
    }
    if (trimmed.startsWith(">")) {
      const quote: string[] = [];
      while (i < lines.length && lines[i].trim().startsWith(">")) quote.push(lines[i++].trim().replace(/^>\s?/, ""));
      blocks.push({ type: "quote", lines: quote });
      continue;
    }
    const bullet = /^([-*+]|\d+[.)])\s+/;
    if (bullet.test(trimmed)) {
      const ordered = /^\d/.test(trimmed);
      const items: string[] = [];
      while (i < lines.length && bullet.test(lines[i].trim()) && /^\d/.test(lines[i].trim()) === ordered) {
        items.push(lines[i++].trim().replace(bullet, ""));
      }
      blocks.push({ type: "list", ordered, items });
      continue;
    }
    const paragraph: string[] = [];
    while (
      i < lines.length &&
      lines[i].trim() &&
      !/^(#{1,6}\s|```|>|[-*+]\s|\d+[.)]\s)/.test(lines[i].trim()) &&
      !(lines[i].includes("|") && i + 1 < lines.length && tableSeparator.test(lines[i + 1].trim()))
    ) {
      paragraph.push(lines[i++].trim());
    }
    if (!paragraph.length) paragraph.push(lines[i++].trim());
    blocks.push({ type: "paragraph", lines: paragraph });
  }
  return blocks;
}

const headingClass: Record<number, string> = {
  1: "text-xl font-semibold mt-6 mb-3",
  2: "text-lg font-semibold mt-6 mb-2.5",
  3: "text-base font-semibold mt-5 mb-2",
};

export function Markdown({ source }: { source: string }) {
  const blocks = parse(source || "");
  return (
    <div className="text-[15px] leading-7 text-zinc-700 dark:text-zinc-300 [&>*:first-child]:mt-0">
      {blocks.map((block, index) => {
        const key = `b${index}`;
        switch (block.type) {
          case "heading":
            return (
              <p key={key} role="heading" aria-level={block.level} className={`${headingClass[block.level] || "font-semibold mt-4 mb-2"} tracking-tight text-zinc-900 dark:text-zinc-50`}>
                {renderInline(block.text, key)}
              </p>
            );
          case "paragraph":
            return (
              <p key={key} className="my-3">
                {block.lines.map((line, lineIndex) => (
                  <Fragment key={lineIndex}>
                    {lineIndex > 0 ? <br /> : null}
                    {renderInline(line, `${key}-${lineIndex}`)}
                  </Fragment>
                ))}
              </p>
            );
          case "list": {
            const ListTag = block.ordered ? "ol" : "ul";
            return (
              <ListTag key={key} className={`my-3 space-y-1 pl-6 ${block.ordered ? "list-decimal" : "list-disc"} marker:text-emerald-500`}>
                {block.items.map((item, itemIndex) => (
                  <li key={itemIndex}>{renderInline(item, `${key}-${itemIndex}`)}</li>
                ))}
              </ListTag>
            );
          }
          case "quote":
            return (
              <blockquote key={key} className="my-3 border-l-2 border-emerald-400 pl-4 text-zinc-600 italic dark:text-zinc-400">
                {block.lines.map((line, lineIndex) => (
                  <Fragment key={lineIndex}>
                    {lineIndex > 0 ? <br /> : null}
                    {renderInline(line, `${key}-${lineIndex}`)}
                  </Fragment>
                ))}
              </blockquote>
            );
          case "code":
            return (
              <pre key={key} className="my-3 overflow-x-auto rounded-lg bg-zinc-900 p-3 font-mono text-[13px] leading-6 text-zinc-100 dark:bg-black/40">
                {block.text}
              </pre>
            );
          case "table":
            return (
              <div key={key} className="my-3 overflow-x-auto">
                <table className="w-full border-collapse text-sm">
                  <thead>
                    <tr>
                      {block.header.map((cell, cellIndex) => (
                        <th key={cellIndex} className="border border-zinc-200 bg-zinc-50 px-3 py-1.5 text-left font-semibold dark:border-zinc-700 dark:bg-zinc-800/60">
                          {renderInline(cell, `${key}-h${cellIndex}`)}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {block.rows.map((row, rowIndex) => (
                      <tr key={rowIndex}>
                        {block.header.map((_, cellIndex) => (
                          <td key={cellIndex} className="border border-zinc-200 px-3 py-1.5 align-top dark:border-zinc-700">
                            {renderInline(row[cellIndex] ?? "", `${key}-${rowIndex}-${cellIndex}`)}
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            );
          case "rule":
            return <hr key={key} className="my-5 border-zinc-200 dark:border-zinc-800" />;
        }
      })}
    </div>
  );
}

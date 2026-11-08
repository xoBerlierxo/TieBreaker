import type { ReactNode } from "react";

// A deliberately tiny Markdown renderer for report prose: headings, bullet
// lists, paragraphs, **bold** and *italic*. It builds React elements (never
// raw HTML), so prose, including LLM prose, can't inject markup or scripts.

function inline(text: string, key: string): ReactNode[] {
  const out: ReactNode[] = [];
  const re = /(\*\*[^*]+\*\*|\*[^*]+\*)/g;
  let last = 0;
  let i = 0;
  for (const m of text.matchAll(re)) {
    if (m.index! > last) out.push(text.slice(last, m.index));
    const token = m[0];
    out.push(
      token.startsWith("**") ? <strong key={`${key}-${i++}`} className="text-white">{token.slice(2, -2)}</strong> : <em key={`${key}-${i++}`}>{token.slice(1, -1)}</em>,
    );
    last = m.index! + token.length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

export function Markdown({ source }: { source: string }) {
  const blocks: ReactNode[] = [];
  let list: string[] = [];
  const flush = () => {
    if (list.length) {
      const items = list;
      blocks.push(
        <ul key={`ul-${blocks.length}`} className="ml-5 list-disc space-y-1 marker:text-emerald-400">
          {items.map((li, i) => (
            <li key={i}>{inline(li, `li-${blocks.length}-${i}`)}</li>
          ))}
        </ul>,
      );
      list = [];
    }
  };
  source.split("\n").forEach((raw, n) => {
    const line = raw.trimEnd();
    if (line.startsWith("- ")) {
      list.push(line.slice(2));
      return;
    }
    flush();
    if (!line.trim()) return;
    const h = /^(#{1,3})\s+(.*)$/.exec(line);
    if (h) {
      const cls = h[1].length === 1 ? "text-2xl font-bold mt-2 text-white" : h[1].length === 2 ? "text-xl font-semibold mt-4 text-white" : "text-lg font-semibold mt-3 text-white";
      blocks.push(
        <p key={n} className={cls}>
          {inline(h[2], `h-${n}`)}
        </p>,
      );
      return;
    }
    blocks.push(<p key={n}>{inline(line, `p-${n}`)}</p>);
  });
  flush();
  return <div className="space-y-2 leading-relaxed text-zinc-300">{blocks}</div>;
}

import { useEffect, useMemo, useRef, useState } from "react";
import { ArrowLeft, Inbox } from "lucide-react";
import { api } from "../lib/api";
import { useApp } from "../lib/context";
import { t } from "../lib/i18n";
import type { DocumentItem, SourceDocument } from "../lib/types";
import { Button, Card, CardHeader, CopyButton, Empty, ErrorNotice, ListSkeleton, PageHeader } from "../components/ui";

// One material on its own page: just the summary and the stored original text.
export default function DocumentDetailView() {
  const { focusTarget, jumpTo } = useApp();
  const id = useMemo(() => {
    const match = /^document:(\d+)$/.exec(focusTarget);
    return match ? Number(match[1]) : 0;
  }, [focusTarget]);

  const [doc, setDoc] = useState<DocumentItem | null>(null);
  const [text, setText] = useState<string | null>(null);
  const [textError, setTextError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);

  const latestID = useRef(id);
  latestID.current = id;

  useEffect(() => {
    setDoc(null);
    setText(null);
    setTextError(null);
    setError(null);
    if (!id) {
      setLoading(false);
      return;
    }
    setLoading(true);
    Promise.all([
      api<{ document: DocumentItem }>(`/api/v1/documents/${id}`),
      api<SourceDocument>(`/api/v1/documents/${id}/text`).catch((reason: unknown) => {
        if (latestID.current === id) setTextError(reason);
        return null;
      }),
    ])
      .then(([detail, source]) => {
        if (latestID.current !== id) return;
        setDoc(detail.document);
        setText(source ? source.text : null);
      })
      .catch((reason) => {
        if (latestID.current === id) setError(reason);
      })
      .finally(() => {
        if (latestID.current === id) setLoading(false);
      });
  }, [id]);

  return (
    <>
      <PageHeader
        title={doc?.filename ?? t("documents.detailTitle")}
        actions={
          <Button variant="secondary" size="sm" icon={ArrowLeft} onClick={() => jumpTo("documents")}>
            {t("documents.back")}
          </Button>
        }
      />

      {error ? (
        <ErrorNotice error={error} />
      ) : loading ? (
        <ListSkeleton rows={2} />
      ) : !doc ? (
        <Empty icon={Inbox} title={t("documents.emptyFilter")} />
      ) : (
        <div className="space-y-4">
          {doc.summary ? (
            <Card>
              <CardHeader title={t("documents.summary")} />
              <p className="px-5 py-4 text-sm leading-7 break-words whitespace-pre-wrap text-zinc-700 dark:text-zinc-300">{doc.summary}</p>
            </Card>
          ) : null}

          <Card>
            <CardHeader
              title={t("documents.original")}
              description={text ? t("documents.sourceChars", { n: text.length }) : undefined}
              actions={text ? <CopyButton text={text} /> : undefined}
            />
            {textError ? (
              <div className="p-4">
                <ErrorNotice error={textError} />
              </div>
            ) : (
              <div className="m-4 max-h-[36rem] overflow-auto rounded-md border border-zinc-200 bg-zinc-50/70 px-4 py-3 dark:border-zinc-800 dark:bg-zinc-900">
                <pre className="font-sans text-sm leading-7 break-words whitespace-pre-wrap text-zinc-700 dark:text-zinc-300">{text ?? ""}</pre>
              </div>
            )}
          </Card>
        </div>
      )}
    </>
  );
}

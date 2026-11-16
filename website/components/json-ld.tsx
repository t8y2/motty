/**
 * Renders a JSON-LD block. `</` is escaped because the payload is embedded in
 * an HTML <script> and a title could otherwise close it early.
 */
export function JsonLd({ data }: { data: Record<string, unknown> | readonly Record<string, unknown>[] }) {
  const json = JSON.stringify(data).replace(/</g, '\\u003c');
  return <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: json }} />;
}

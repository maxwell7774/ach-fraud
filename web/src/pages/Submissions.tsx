import { createResource, For, Show } from "solid-js";
import { useSearchParams, A } from "@solidjs/router";
import { api, fmtDateTime } from "../api";
import { Badge, EmptyState, Loading } from "../components";

const STATUSES = ["", "received", "ready", "failed"];

export default function Submissions() {
  const [params, setParams] = useSearchParams();
  const status = () => { const s = params.status; return typeof s === "string" ? s : ""; };
  const [subs] = createResource(status, (s) => api.submissions(s || undefined));

  function setStatus(s: string) {
    setParams({ status: s || undefined });
  }

  return (
    <>
      <h1>Files</h1>
      <div class="dir-tabs">
        <For each={STATUSES}>
          {(s) => (
            <a
              class={`dir-tab ${s === status() ? "active" : ""}`}
              href={s === "" ? "/submissions" : `/submissions?status=${s}`}
              onClick={(e) => {
                e.preventDefault();
                setStatus(s);
              }}
            >
              {s === "" ? "All" : s}
            </a>
          )}
        </For>
      </div>
      <Show when={subs()} fallback={<Loading label="Loading files…" />}>
        <div class="table-wrap">
          <Show
            when={subs()!.length > 0}
            fallback={
              <EmptyState
                message="No files found."
                hint="Files appear here as they are ingested from the input directory."
              />
            }
          >
            <table class="responsive">
              <thead>
                <tr>
                  <th>Status</th>
                  <th>File</th>
                  <th>Received</th>
                </tr>
              </thead>
              <tbody>
                <For each={subs()!}>
                  {(s) => (
                    <tr>
                      <td data-label="Status">
                        <Badge status={s.status} />
                      </td>
                      <td data-label="File">
                        <A href={`/submissions/${s.id}`} class="file-link">
                          {s.filename}
                        </A>
                      </td>
                      <td data-label="Received" class="muted">
                        {fmtDateTime(s.received_at)}
                      </td>
                    </tr>
                  )}
                </For>
              </tbody>
            </table>
          </Show>
        </div>
      </Show>
    </>
  );
}

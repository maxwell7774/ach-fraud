import { createMemo, createResource, createSignal, For, Show } from "solid-js";
import { api, fmtDateTime, human } from "../api";
import { EmptyState, Loading, Pagination } from "../components";

export default function Events() {
  const [page, setPage] = createSignal(1);
  const [pageSize, setPageSize] = createSignal(25);

  const key = createMemo(() => ({ page: page(), pageSize: pageSize() }));
  const [data] = createResource(key, api.events);

  return (
    <>
      <h1>Events</h1>
      <Show when={data()} fallback={<Loading label="Loading events…" />}>
        <div class="table-wrap">
          <Show
            when={data()!.events.length > 0}
            fallback={<EmptyState message="No events recorded yet." />}
          >
            <table class="responsive">
              <thead>
                <tr>
                  <th>Type</th>
                  <th>When</th>
                  <th>Details</th>
                </tr>
              </thead>
              <tbody>
                <For each={data()!.events}>
                  {(e) => (
                    <tr>
                      <td data-label="Type">{human(e.type)}</td>
                      <td data-label="When" class="muted">{fmtDateTime(e.created_at)}</td>
                      <td data-label="Details" class="muted">
                        {e.payload ? JSON.stringify(e.payload).slice(0, 120) : e.ref ?? "—"}
                      </td>
                    </tr>
                  )}
                </For>
              </tbody>
            </table>
            <Pagination
              page={page()}
              pageSize={pageSize()}
              total={data()?.total ?? 0}
              onChange={(p, s) => {
                setPage(p);
                setPageSize(s);
              }}
            />
          </Show>
        </div>
      </Show>
    </>
  );
}

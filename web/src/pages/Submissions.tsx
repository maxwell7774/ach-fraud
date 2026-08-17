import { createMemo, createSignal, For, Show } from "solid-js";
import { useSearchParams, A } from "@solidjs/router";
import { createAsync } from "@solidjs/router";
import { submissionsQuery, usePolling } from "../queries";
import { api, fmtDateTime } from "../api";
import { Badge, EmptyState, Loading, Pagination, SortableTh, DatePicker, PageHeader } from "../components";

const STATUSES = ["", "received", "ready", "archived", "failed"];

export default function Submissions() {
  const { remaining } = usePolling("submissions");
  const [params, setParams] = useSearchParams();
  const status = () => {
    const s = params.status;
    return typeof s === "string" ? s : "";
  };
  const [search, setSearch] = createSignal("");
  const [q, setQ] = createSignal("");
  const [startDate, setStartDate] = createSignal("");
  const [endDate, setEndDate] = createSignal("");
  const [page, setPage] = createSignal(1);
  const [pageSize, setPageSize] = createSignal(25);
  const [sort, setSort] = createSignal("");
  const [dir, setDir] = createSignal("");

  const key = createMemo(() => ({
    status: status() || undefined,
    q: q() || undefined,
    start_date: startDate() || undefined,
    end_date: endDate() || undefined,
    page: page(),
    pageSize: pageSize(),
    sort: sort() || undefined,
    dir: dir() || undefined,
  }));
  const data = createAsync(() => submissionsQuery(key()));

  function setStatus(s: string) {
    setParams({ status: s || undefined });
    setPage(1);
  }
  function applyFilters() {
    setQ(search());
    setPage(1);
  }
  function onSort(col: string, d: string) {
    setSort(col);
    setDir(d);
    setPage(1);
  }

  return (
    <>
      <PageHeader remaining={remaining}>
        <h1>Files</h1>
      </PageHeader>
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
      <div class="search-bar">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            applyFilters();
          }}
        >
          <input
            type="search"
            placeholder="Search filename, checksum…"
            value={search()}
            onInput={(e) => setSearch(e.currentTarget.value)}
          />
          <label>
            From <DatePicker value={startDate()} onChange={setStartDate} />
          </label>
          <label>
            To <DatePicker value={endDate()} onChange={setEndDate} />
          </label>
          <button class="btn btn-outline" type="submit">
            Search
          </button>
        </form>
      </div>
      <Show when={data()} fallback={<Loading label="Loading files…" />}>
        <div class="table-wrap">
          <Show
            when={data()!.submissions.length > 0}
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
                  <SortableTh col="status" sort={sort()} dir={dir()} onSort={onSort}>
                    Status
                  </SortableTh>
                  <SortableTh col="filename" sort={sort()} dir={dir()} onSort={onSort}>
                    File
                  </SortableTh>
                  <SortableTh col="received" sort={sort()} dir={dir()} onSort={onSort}>
                    Received
                  </SortableTh>
                </tr>
              </thead>
              <tbody>
                <For each={data()!.submissions}>
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
            <Pagination
              page={page()}
              pageSize={pageSize()}
              total={data()?.total ?? 0}
              onChange={(p, sz) => {
                setPage(p);
                setPageSize(sz);
              }}
            />
          </Show>
        </div>
      </Show>
    </>
  );
}

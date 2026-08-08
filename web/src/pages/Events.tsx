import { createMemo, createSignal, For, Show } from "solid-js";
import { createAsync } from "@solidjs/router";
import { eventsQuery } from "../queries";
import { api, fmtDateTime, human } from "../api";
import { EmptyState, Loading, Pagination, SortableTh, DatePicker } from "../components";

export default function Events() {
  const [search, setSearch] = createSignal("");
  const [q, setQ] = createSignal("");
  const [startDate, setStartDate] = createSignal("");
  const [endDate, setEndDate] = createSignal("");
  const [page, setPage] = createSignal(1);
  const [pageSize, setPageSize] = createSignal(25);
  const [sort, setSort] = createSignal("");
  const [dir, setDir] = createSignal("");

  const key = createMemo(() => ({
    q: q() || undefined,
    start_date: startDate() || undefined,
    end_date: endDate() || undefined,
    page: page(),
    pageSize: pageSize(),
    sort: sort() || undefined,
    dir: dir() || undefined,
  }));
  const data = createAsync(() => eventsQuery(key()));

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
      <h1>Events</h1>
      <div class="search-bar">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            applyFilters();
          }}
        >
          <input
            type="search"
            placeholder="Search event type…"
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
            Filter
          </button>
        </form>
      </div>
      <Show when={data()} fallback={<Loading label="Loading events…" />}>
        <div class="table-wrap">
          <Show
            when={data()!.events.length > 0}
            fallback={<EmptyState message="No events recorded yet." />}
          >
            <table class="responsive">
              <thead>
                <tr>
                  <SortableTh col="type" sort={sort()} dir={dir()} onSort={onSort}>
                    Type
                  </SortableTh>
                  <SortableTh col="when" sort={sort()} dir={dir()} onSort={onSort}>
                    When
                  </SortableTh>
                  <th>Details</th>
                </tr>
              </thead>
              <tbody>
                <For each={data()!.events}>
                  {(e) => (
                    <tr>
                      <td data-label="Type">{human(e.type)}</td>
                      <td data-label="When" class="muted">
                        {fmtDateTime(e.created_at)}
                      </td>
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

import { createMemo, createSignal, For, Show } from "solid-js";
import { createAsync } from "@solidjs/router";
import { headersQuery } from "../queries";
import { api, fmtDate } from "../api";
import { EmptyState, Loading, Pagination, SortableTh, DatePicker } from "../components";

export default function Headers() {
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
  const data = createAsync(() => headersQuery(key()));

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
      <h1>Headers</h1>
      <div class="search-bar">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            applyFilters();
          }}
        >
          <input
            type="search"
            placeholder="Search company, customer id, description, file…"
            value={search()}
            onInput={(e) => setSearch(e.currentTarget.value)}
          />
          <label>
            From{" "}
            <DatePicker value={startDate()} onChange={setStartDate} />
          </label>
          <label>
            To{" "}
            <DatePicker value={endDate()} onChange={setEndDate} />
          </label>
          <button class="btn btn-outline" type="submit">
            Filter
          </button>
        </form>
      </div>
      <Show when={data()} fallback={<Loading label="Loading headers…" />}>
        <div class="table-wrap">
          <Show
            when={data()!.headers.length > 0}
            fallback={
              <EmptyState
                message="No batch headers found."
                hint="Adjust the search or clear the filters."
              />
            }
          >
            <table class="responsive">
              <thead>
                <tr>
                  <SortableTh col="company_name" sort={sort()} dir={dir()} onSort={onSort}>Company</SortableTh>
                  <SortableTh col="customer_id" sort={sort()} dir={dir()} onSort={onSort}>Customer ID</SortableTh>
                  <th>Description</th>
                  <SortableTh col="effective" sort={sort()} dir={dir()} onSort={onSort}>Effective</SortableTh>
                  <SortableTh col="filename" sort={sort()} dir={dir()} onSort={onSort}>File</SortableTh>
                </tr>
              </thead>
              <tbody>
                <For each={data()!.headers}>
                  {(h) => (
                    <tr>
                      <td data-label="Company">{h.company_name}</td>
                      <td data-label="Customer ID">{h.customer_id}</td>
                      <td data-label="Description">{h.company_description}</td>
                      <td data-label="Effective">{fmtDate(h.effective_date)}</td>
                      <td data-label="File" class="muted">{h.filename}</td>
                    </tr>
                  )}
                </For>
              </tbody>
            </table>
            <Pagination page={page()} pageSize={pageSize()} total={data()?.total ?? 0} onChange={(p, s) => { setPage(p); setPageSize(s); }} />
          </Show>
        </div>
      </Show>
    </>
  );
}

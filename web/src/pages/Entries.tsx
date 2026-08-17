import { createMemo, createSignal, For, Show } from "solid-js";
import { createAsync, revalidate } from "@solidjs/router";
import { entriesQuery } from "../queries";
import { api, dollars, fmtDate } from "../api";
import { EmptyState, Loading, Pagination, SortableTh, DatePicker, QueryError } from "../components";

export default function Entries() {
  const [search, setSearch] = createSignal("");
  const [q, setQ] = createSignal("");
  const [startDate, setStartDate] = createSignal("");
  const [endDate, setEndDate] = createSignal("");
  const [page, setPage] = createSignal(1);
  const [pageSize, setPageSize] = createSignal(25);
  const [sort, setSort] = createSignal("");
  const [dir, setDir] = createSignal("");
  const [queryError, setQueryError] = createSignal<unknown>();

  const key = createMemo(() => ({
    q: q() || undefined,
    start_date: startDate() || undefined,
    end_date: endDate() || undefined,
    page: page(),
    pageSize: pageSize(),
    sort: sort() || undefined,
    dir: dir() || undefined,
  }));
  const data = createAsync(() => { setQueryError(undefined); return entriesQuery(key()).catch((e) => { setQueryError(e); return undefined; }); });

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
      <h1>Entries</h1>
      <div class="search-bar">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            applyFilters();
          }}
        >
          <input
            type="search"
            placeholder="Search trace, rdfi, receiver, account, file…"
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
            Search
          </button>
        </form>
      </div>
      <Show when={data()} fallback={<Show when={queryError()} fallback={<Loading label="Loading entries…" />}><QueryError error={queryError()} onRetry={() => revalidate("entries")} /></Show>}>
        <div class="table-wrap">
          <Show
            when={data()!.entries.length > 0}
            fallback={
              <EmptyState
                message="No entries found."
                hint="Adjust the search or clear the filters."
              />
            }
          >
            <table class="responsive">
              <thead>
                <tr>
                  <SortableTh col="trace" sort={sort()} dir={dir()} onSort={onSort}>Trace</SortableTh>
                  <SortableTh col="rdfi" sort={sort()} dir={dir()} onSort={onSort}>RDFI</SortableTh>
                  <SortableTh col="receiver" sort={sort()} dir={dir()} onSort={onSort}>Receiver</SortableTh>
                  <SortableTh col="account" sort={sort()} dir={dir()} onSort={onSort}>Account</SortableTh>
                  <SortableTh col="amount" sort={sort()} dir={dir()} onSort={onSort}>Amount</SortableTh>
                  <SortableTh col="tran_code" sort={sort()} dir={dir()} onSort={onSort}>Tran</SortableTh>
                  <SortableTh col="effective" sort={sort()} dir={dir()} onSort={onSort}>Effective</SortableTh>
                  <SortableTh col="filename" sort={sort()} dir={dir()} onSort={onSort}>File</SortableTh>
                </tr>
              </thead>
              <tbody>
                <For each={data()!.entries}>
                  {(e) => (
                    <tr>
                      <td data-label="Trace">{e.trace}</td>
                      <td data-label="RDFI">{e.rdfi}</td>
                      <td data-label="Receiver">{e.receiver_name}</td>
                      <td data-label="Account">{e.receiver_account}</td>
                      <td data-label="Amount" class="amount">{dollars(e.amount)}</td>
                      <td data-label="Tran">{e.tran_code}</td>
                      <td data-label="Effective">{fmtDate(e.effective_date)}</td>
                      <td data-label="File" class="muted">{e.filename}</td>
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

import { createMemo, createSignal, For, Show } from "solid-js";
import { createAsync, useSearchParams, useAction, useSubmission, A } from "@solidjs/router";
import { dollars, fmtDate, human } from "../api";
import type { Hold } from "../api";
import { Badge, Pagination, RowActions, SortableTh, DatePicker, EmptyState, Loading, PageHeader } from "../components";
import { useFlash } from "../flash";
import { useConfirm } from "../confirm";
import { canReview } from "../user";
import { approveHoldAction, declineHoldAction, bulkHoldAction, dashboardQuery, holdsQuery, usePolling } from "../queries";

const STATUSES = ["", "pending", "approved", "declined", "auto_declined"];

function groupKey(h: Hold): string {
  return [h.entry_receiver_account, h.entry_rdfi, h.effective_date ?? "", h.customer_id].join("|");
}

export default function Holds() {
  const { remaining } = usePolling("dashboard", "holds");
  const [params, setParams] = useSearchParams();
  // No status in the URL defaults to the pending view; the "All" tab is the
  // explicit `status=all`.
  const status = () => {
    const s = params.status;
    if (s === "" || s === "all") return "";
    if (typeof s === "string") return s;
    return "pending";
  };
  const { show: flash } = useFlash();
  const { confirm } = useConfirm();

  const [search, setSearch] = createSignal("");
  const [q, setQ] = createSignal("");
  const [startDate, setStartDate] = createSignal("");
  const [endDate, setEndDate] = createSignal("");
  const [page, setPage] = createSignal(1);
  const [pageSize, setPageSize] = createSignal(25);
  const [sort, setSort] = createSignal("");
  const [dir, setDir] = createSignal("");
  const [selected, setSelected] = createSignal<Set<string>>(new Set<string>());
  const dash = createAsync(() => dashboardQuery());

  const tabCount = (s: string) => {
    const counts = dash()?.hold_counts ?? {};
    if (s === "") return Object.values(counts).reduce((a, b) => a + b, 0);
    return counts[s] ?? 0;
  };

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
  const data = createAsync(() => holdsQuery(key()));

  const rows = () => data()?.holds ?? [];
  const ids = () => rows().map((h) => h.id);

  // Number of holds on the current page sharing the same velocity group.
  const groupCounts = createMemo(() => {
    const m = new Map<string, number>();
    for (const h of rows()) {
      const k = groupKey(h);
      m.set(k, (m.get(k) ?? 0) + 1);
    }
    return m;
  });

  const approve = useAction(approveHoldAction);
  const decline = useAction(declineHoldAction);
  const bulk = useAction(bulkHoldAction);
  const bulkSub = useSubmission(bulkHoldAction);

  function setStatus(s: string) {
    setParams({ status: s === "" ? "all" : s });
    setPage(1);
    setSelected(new Set<string>());
  }
  function applyFilters() {
    setQ(search());
    setPage(1);
  }
  function clearFilters() {
    setSearch("");
    setQ("");
    setStartDate("");
    setEndDate("");
    setParams({ status: "pending" });
    setPage(1);
  }
  // The status tab is a view, not a filter — only search/dates count as active.
  const filtersActive = () => !!(q() || startDate() || endDate());
  function onSort(col: string, d: string) {
    setSort(col);
    setDir(d);
    setPage(1);
  }
  function onPage(p: number, s: number) {
    setPage(p);
    setPageSize(s);
  }
  function toggle(id: string, checked: boolean) {
    const s = new Set(selected());
    if (checked) s.add(id);
    else s.delete(id);
    setSelected(s);
  }
  function toggleAll(checked: boolean) {
    const s = new Set(selected());
    if (checked) ids().forEach((id) => s.add(id));
    else ids().forEach((id) => s.delete(id));
    setSelected(s);
  }
  const allChecked = () => ids().length > 0 && ids().every((id) => selected().has(id));

  // groupWarning builds the confirm-dialog warning for holds in a velocity
  // group, listing every member that will be decided together.
  function groupWarning(h: Hold, action: "approve" | "decline"): string {
    const members = rows().filter((r) => groupKey(r) === groupKey(h));
    if (members.length <= 1) return "";
    const lines = members.map(
      (m) => `• ${dollars(m.entry_amount)} to ${m.entry_receiver_account}${m.entry_receiver_name ? ` (${m.entry_receiver_name})` : ""}`
    );
    return (
      `\n\nThis hold is part of a velocity group of ${members.length}. ` +
      `The ${action === "approve" ? "approval" : "decline"} applies to every hold in the group:\n` +
      lines.join("\n")
    );
  }

  async function actOne(h: Hold, action: "approve" | "decline") {
    const lines = [
      `Account: ${h.entry_receiver_account}`,
      h.entry_receiver_name ? `Name: ${h.entry_receiver_name}` : null,
      `Amount: ${dollars(h.entry_amount)}`,
      `RDFI: ${h.entry_rdfi}`,
      h.customer_id ? `Customer: ${h.customer_id}` : null,
      h.reason ? `Reason held: ${h.reason}` : null,
    ].filter(Boolean);
    const msg = `${action === "approve" ? "Approve" : "Decline"} this hold?\n\n${lines.join("\n")}${groupWarning(h, action)}`;
    const res = await confirm(msg, {
      title: `${action === "approve" ? "Approve" : "Decline"} hold`,
      tone: action,
      confirmLabel: action === "approve" ? "Approve" : "Decline",
      showNote: true,
    });
    if (!res.ok) return;
    const submit = action === "approve" ? approve : decline;
    const result = await submit({ id: h.id, note: res.note });
    if (!result.ok) {
      flash("error", result.error ?? "failed");
      return;
    }
    const members = rows().filter((r) => groupKey(r) === groupKey(h));
    const decided = members.length > 1 ? members : [h];
    const total = decided.reduce((sum, m) => sum + m.entry_amount, 0);
    const holdLines = decided.map(
      (m) => `${dollars(m.entry_amount)} to ${m.entry_receiver_account}`
    );
    flash("success", `${action === "approve" ? "Approved" : "Declined"} ${decided.length} hold${decided.length > 1 ? "s" : ""} (${dollars(total)})\n${holdLines.join("\n")}`);
    setSelected(new Set<string>());
  }

  async function actBulk(action: "approve" | "decline") {
    const ids = [...selected()];
    if (ids.length === 0) return;
    const selectedHolds = rows().filter((h) => selected().has(h.id));
    const total = selectedHolds.reduce((sum, h) => sum + h.entry_amount, 0);
    const accounts = [...new Set(selectedHolds.map((h) => h.entry_receiver_account))];
    const msg = `${action === "approve" ? "Approve" : "Decline"} ${ids.length} holds totaling ${dollars(total)}?\n\nAccounts: ${accounts.join(", ")}`;
    const res = await confirm(msg, {
      title: `${action === "approve" ? "Approve" : "Decline"} ${ids.length} holds`,
      tone: action,
      confirmLabel: action === "approve" ? "Approve" : "Decline",
      showNote: true,
    });
    if (!res.ok) return;
    const result = await bulk({ action, ids });
    if (!result.ok) {
      flash("error", result.error ?? "failed");
      return;
    }
    flash("success", `${action === "approve" ? "Approved" : "Declined"} ${result.count} holds (${dollars(total)})`);
    setSelected(new Set<string>());
  }

  return (
    <>
      <PageHeader remaining={remaining}>
        <h1>Holds</h1>
      </PageHeader>

      <div class="dir-tabs">
        <For each={STATUSES}>
          {(s) => (
            <a
              class={`dir-tab ${s === status() ? "active" : ""}`}
              href={s === "" ? "/holds?status=all" : `/holds?status=${s}`}
              onClick={(e) => {
                e.preventDefault();
                setStatus(s);
              }}
            >
              {s === "" ? "All" : human(s)}
              <Show when={s === "pending" && tabCount(s) > 0}>
                <span class="tab-count">{tabCount(s)}</span>
              </Show>
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
            placeholder="Search account, name, trace, file…"
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

      <Show when={data()} fallback={<Loading label="Loading holds…" />}>
        <div class="table-wrap">
          <Show when={rows().length === 0}>
            <EmptyState
              message={filtersActive() ? "No holds match your filters." : "No holds yet."}
              hint={
                filtersActive()
                  ? "Try a different search or clear the filters below."
                  : "Holds appear here when a file is screened."
              }
            />
            <Show when={filtersActive()}>
              <div class="empty-actions">
                <button class="btn btn-outline btn-sm" onClick={clearFilters}>
                  Clear filters
                </button>
              </div>
            </Show>
          </Show>
          <Show when={rows().length > 0}>
          <Show when={canReview() && selected().size > 0}>
            <div class="bulk-bar">
              <span class="selected-count">{selected().size} selected</span>
              <button
                class="btn btn-approve btn-sm"
                disabled={bulkSub.pending}
                onClick={() => actBulk("approve")}
              >
                Approve
              </button>
              <button
                class="btn btn-decline btn-sm"
                disabled={bulkSub.pending}
                onClick={() => actBulk("decline")}
              >
                Decline
              </button>
            </div>
          </Show>
          <table class="responsive">
            <thead>
              <tr>
                <Show when={canReview()}>
                  <th class="select-col">
                    <input
                      type="checkbox"
                      id="selAll"
                      checked={allChecked()}
                      onChange={(e) => toggleAll(e.currentTarget.checked)}
                    />
                  </th>
                </Show>
                <SortableTh col="account" sort={sort()} dir={dir()} onSort={onSort}>
                  Account
                </SortableTh>
                <SortableTh col="amount" sort={sort()} dir={dir()} onSort={onSort}>
                  Amount
                </SortableTh>
                <SortableTh col="effective" sort={sort()} dir={dir()} onSort={onSort}>
                  Effective
                </SortableTh>
                <SortableTh col="rdfi" sort={sort()} dir={dir()} onSort={onSort}>
                  RDFI
                </SortableTh>
                <SortableTh col="customer" sort={sort()} dir={dir()} onSort={onSort}>
                  Customer
                </SortableTh>
                <SortableTh col="status" sort={sort()} dir={dir()} onSort={onSort}>
                  Status
                </SortableTh>
                <th>Why held</th>
                <Show when={canReview()}>
                  <th>Actions</th>
                </Show>
              </tr>
            </thead>
            <tbody>
              <For each={rows()}>
                {(h) => (
                  <tr class="selectable-row">
                    <Show when={canReview()}>
                      <td data-label="">
                        <input
                          type="checkbox"
                          name="id"
                          checked={selected().has(h.id)}
                          onChange={(e) => toggle(h.id, e.currentTarget.checked)}
                        />
                      </td>
                    </Show>
                    <td data-label="Account">
                      <A href={`/holds/${h.id}`} class="file-link">
                        {h.entry_receiver_account}
                      </A>
                      <Show when={h.entry_receiver_name}>
                        <span class="cell-sub">{h.entry_receiver_name}</span>
                      </Show>
                      <Show when={groupCounts().get(groupKey(h))! > 1}>
                        <span
                          class="badge badge-group"
                          title="Same-day velocity group: these hold together, and are released together once approved"
                        >
                          {groupCounts().get(groupKey(h))} in group
                        </span>
                      </Show>
                    </td>
                    <td data-label="Amount" class="amount">
                      {dollars(h.entry_amount)}
                    </td>
                    <td data-label="Effective">{fmtDate(h.effective_date)}</td>
                    <td data-label="RDFI">{h.entry_rdfi}</td>
                    <td data-label="Customer">
                      <span class="muted truncate">{h.customer_id || "—"}</span>
                      <Show when={h.company_name}>
                        <span class="cell-sub">{h.company_name}</span>
                      </Show>
                    </td>
                    <td data-label="Status">
                      <Badge status={h.status} />
                    </td>
                    <td data-label="Why held" class="muted truncate" title={h.reason || ""}>
                      {h.reason || "—"}
                    </td>
                    <Show when={canReview()}>
                      <td data-label="Actions">
                        <Show when={h.status === "pending" || h.status === "auto_declined"}>
                          <RowActions
                            holdId={h.id}
                            onApprove={() => actOne(h, "approve")}
                            onDecline={() => actOne(h, "decline")}
                          />
                        </Show>
                      </td>
                    </Show>
                  </tr>
                )}
              </For>
            </tbody>
          </table>
          <Pagination
            page={page()}
            pageSize={pageSize()}
            total={data()?.total ?? 0}
            onChange={onPage}
          />
          </Show>
        </div>
      </Show>
    </>
  );
}

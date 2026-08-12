import { createMemo, createSignal, For, Show } from "solid-js";
import { A } from "@solidjs/router";
import { Hold, dollars, human } from "./api";
import {
  ArrowDownIcon,
  ArrowUpIcon,
  CalendarIcon,
  CheckIcon,
  ChevronDownIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  ChevronUpDownIcon,
  EllipsisIcon,
  XIcon,
} from "./icons";

export function Badge(props: { status: string }) {
  return <span class={`badge badge-${props.status}`}>{human(props.status)}</span>;
}

export function EmptyState(props: { message: string; hint?: string }) {
  return (
    <div class="empty-state">
      <p class="empty">{props.message}</p>
      <Show when={props.hint}>
        <p class="empty-hint">{props.hint}</p>
      </Show>
    </div>
  );
}

export function Loading(props: { label?: string }) {
  return (
    <p class="empty loading">
      <span class="spinner" aria-hidden="true" />
      {props.label ?? "Loading…"}
    </p>
  );
}

const CARD_DESCRIPTIONS: Record<string, string> = {
  pending: "Awaiting your review",
  approved: "Released · last 7 days",
  declined: "Not released · last 7 days",
  auto_declined: "Screened out · last 7 days",
};

export function Cards(props: { counts: Record<string, number> }) {
  const statuses = ["pending", "approved", "declined", "auto_declined"];
  return (
    <div class="cards">
      <For each={statuses}>
        {(s) => (
          <A class={`card card-${s}`} href={`/holds?status=${s}`}>
            <div class="count">{props.counts[s] ?? 0}</div>
            <div class="label">{human(s)}</div>
            <div class="card-desc">{CARD_DESCRIPTIONS[s] ?? ""}</div>
          </A>
        )}
      </For>
    </div>
  );
}

export function SortableTh(props: {
  col: string;
  sort: string;
  dir: string;
  onSort: (col: string, dir: string) => void;
  children: any;
}) {
  const active = createMemo(() => props.sort === props.col);
  const cls = createMemo(() =>
    active() ? (props.dir === "desc" ? "sort-desc" : "sort-asc") : "sortable"
  );
  function click() {
    props.onSort(props.col, active() && props.dir === "asc" ? "desc" : "asc");
  }
  return (
    <th class={cls()} onClick={click}>
      {props.children}
      <span class="sort-icon">
        {active() ? (
          props.dir === "desc" ? (
            <ArrowDownIcon />
          ) : (
            <ArrowUpIcon />
          )
        ) : (
          <ChevronUpDownIcon />
        )}
      </span>
    </th>
  );
}

export function Pagination(props: {
  page: number;
  pageSize: number;
  total: number;
  onChange: (page: number, size: number) => void;
}) {
  const pages = () => Math.max(1, Math.ceil(props.total / props.pageSize));
  return (
    <div class="pagination">
      <span class="info">
        Page {props.page} of {pages()} · {props.total} rows
      </span>
      <div class="pages">
        <button
          class="btn btn-sm btn-icon"
          aria-label="Previous page"
          disabled={props.page <= 1}
          onClick={() => props.onChange(props.page - 1, props.pageSize)}
        >
          <ChevronLeftIcon />
        </button>
        <button
          class="btn btn-sm btn-icon"
          aria-label="Next page"
          disabled={props.page >= pages()}
          onClick={() => props.onChange(props.page + 1, props.pageSize)}
        >
          <ChevronRightIcon />
        </button>
      </div>
      <label class="page-size">
        <span>Page size</span>
        <Select
          value={String(props.pageSize)}
          onChange={(v) => props.onChange(1, Number(v))}
          options={[
            { value: "25", label: "25" },
            { value: "50", label: "50" },
            { value: "100", label: "100" },
          ]}
        />
      </label>
    </div>
  );
}

let menuSeq = 0;
let dateSeq = 0;

const WEEKDAYS = ["Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"];
const MONTHS = [
  "Jan", "Feb", "Mar", "Apr", "May", "Jun",
  "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
];

function MonthGrid(props: {
  view: { y: number; m: number };
  onShiftYear: (delta: number) => void;
  onPickMonth: (m: number) => void;
  onOpenYear: () => void;
}) {
  return (
    <div>
      <div class="datepicker-head">
        <button type="button" class="datepicker-nav" aria-label="Previous year" onClick={() => props.onShiftYear(-1)}>
          <ChevronLeftIcon />
        </button>
        <button type="button" class="datepicker-title-btn" onClick={() => props.onOpenYear()}>
          {props.view.y}
        </button>
        <button type="button" class="datepicker-nav" aria-label="Next year" onClick={() => props.onShiftYear(1)}>
          <ChevronRightIcon />
        </button>
      </div>
      <div class="monthpicker-grid">
        <For each={MONTHS}>
          {(name, i) => (
            <button
              class={`month-cell ${i() === props.view.m ? "selected" : ""}`}
              type="button"
              onClick={() => props.onPickMonth(i())}
            >
              {name}
            </button>
          )}
        </For>
      </div>
    </div>
  );
}

function YearGrid(props: {
  year: number;
  onShiftDecade: (delta: number) => void;
  onPickYear: (y: number) => void;
}) {
  const start = () => Math.floor(props.year / 10) * 10;
  const years = () => Array.from({ length: 12 }, (_, i) => start() + i);
  return (
    <div>
      <div class="datepicker-head">
        <button type="button" class="datepicker-nav" aria-label="Previous decade" onClick={() => props.onShiftDecade(-1)}>
          <ChevronLeftIcon />
        </button>
        <span class="datepicker-title">
          {start()}–{start() + 11}
        </span>
        <button type="button" class="datepicker-nav" aria-label="Next decade" onClick={() => props.onShiftDecade(1)}>
          <ChevronRightIcon />
        </button>
      </div>
      <div class="yearpicker-grid">
        <For each={years()}>
          {(y) => (
            <button
              class={`year-cell ${y === props.year ? "selected" : ""}`}
              type="button"
              onClick={() => props.onPickYear(y)}
            >
              {y}
            </button>
          )}
        </For>
      </div>
    </div>
  );
}

// DatePicker: a native popover calendar. Value is "YYYY-MM-DD" or "".
export function DatePicker(props: {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
}) {
  const [id] = createSignal(`date-${++dateSeq}`);
  const menuId = () => `${id()}-menu`;
  const now = new Date();
  const [mode, setMode] = createSignal<"day" | "month" | "year">("day");
  const [view, setView] = createSignal({
    y: props.value ? Number(props.value.slice(0, 4)) : now.getFullYear(),
    m: props.value ? Number(props.value.slice(5, 7)) - 1 : now.getMonth(),
  });

  const daysInMonth = () => new Date(view().y, view().m + 1, 0).getDate();
  const firstDay = () => new Date(view().y, view().m, 1).getDay();
  const monthTitle = () =>
    new Date(view().y, view().m, 1).toLocaleDateString("en-US", { month: "long", year: "numeric" });

  function shiftMonth(delta: number) {
    setView((v) => {
      const d = new Date(v.y, v.m + delta, 1);
      return { y: d.getFullYear(), m: d.getMonth() };
    });
  }
  function shiftYear(delta: number) {
    setView((v) => ({ y: v.y + delta, m: v.m }));
  }
  function shiftDecade(delta: number) {
    setView((v) => ({ y: v.y + delta * 12, m: v.m }));
  }
  function pickMonth(m: number) {
    setView((v) => ({ y: v.y, m }));
    setMode("day");
  }
  function pickYear(y: number) {
    setView((v) => ({ y, m: v.m }));
    setMode("month");
  }
  function close() {
    document.getElementById(menuId())?.hidePopover();
  }
  function pick(day: number) {
    const iso = `${view().y}-${String(view().m + 1).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
    props.onChange(iso);
    close();
  }
  function clear() {
    props.onChange("");
    close();
  }

  return (
    <>
      <button
        id={`${id()}-btn`}
        class={`date-picker ${props.value ? "" : "is-empty"}`}
        type="button"
        popovertarget={menuId()}
      >
        <span class="date-picker-icon">
          <CalendarIcon />
        </span>
        <span class="date-picker-value">
          {props.value ? props.value : (props.placeholder ?? "Any date")}
        </span>
        <span class="caret">
          <ChevronDownIcon />
        </span>
      </button>
      <div id={menuId()} popover="auto" class="popover-menu datepicker-menu">
        <Show
          when={mode() === "day"}
          fallback={
            <Show
              when={mode() === "month"}
              fallback={
                <YearGrid year={view().y} onShiftDecade={shiftDecade} onPickYear={pickYear} />
              }
            >
              <MonthGrid
                view={view()}
                onShiftYear={shiftYear}
                onPickMonth={pickMonth}
                onOpenYear={() => setMode("year")}
              />
            </Show>
          }
        >
          <div class="datepicker-head">
            <button type="button" class="datepicker-nav" aria-label="Previous month" onClick={() => shiftMonth(-1)}>
              <ChevronLeftIcon />
            </button>
            <button type="button" class="datepicker-title-btn" onClick={() => setMode("month")}>
              {monthTitle()}
            </button>
            <button type="button" class="datepicker-nav" aria-label="Next month" onClick={() => shiftMonth(1)}>
              <ChevronRightIcon />
            </button>
          </div>
          <div class="datepicker-grid">
            <For each={WEEKDAYS}>{(d) => <span class="dow">{d}</span>}</For>
            <For each={Array.from({ length: firstDay() })}>{() => <span />}</For>
            <For each={Array.from({ length: daysInMonth() }, (_, i) => i + 1)}>
              {(day) => {
                const iso = `${view().y}-${String(view().m + 1).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
                return (
                  <button
                    class={`date-cell ${iso === props.value ? "selected" : ""}`}
                    type="button"
                    onClick={() => pick(day)}
                  >
                    {day}
                  </button>
                );
              }}
            </For>
          </div>
        </Show>
        <div class="datepicker-foot">
          <button type="button" class="btn btn-outline btn-sm" onClick={clear}>
            Clear
          </button>
        </div>
      </div>
    </>
  );
}

export function Select(props: {
  value: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
}) {
  const [id] = createSignal(`sel-${++menuSeq}`);
  const menuId = () => `${id()}-menu`;
  const current = () => props.options.find((o) => o.value === props.value);
  function choose(v: string) {
    props.onChange(v);
    document.getElementById(menuId())?.hidePopover();
  }
  return (
    <>
      <button
        id={`${id()}-btn`}
        class="custom-select"
        type="button"
        popovertarget={menuId()}
      >
        <span>{current()?.label ?? props.value}</span>
        <span class="caret">
          <ChevronDownIcon />
        </span>
      </button>
      <div id={menuId()} popover="auto" class="popover-menu custom-select-menu">
        <For each={props.options}>
          {(o) => (
            <button
              class={`custom-select-option ${o.value === props.value ? "active" : ""}`}
              type="button"
              onClick={() => choose(o.value)}
            >
              {o.label}
            </button>
          )}
        </For>
      </div>
    </>
  );
}

export function RowMenu(props: {
  actions: { label: string; onClick: () => void; danger?: boolean }[];
}) {
  const [id] = createSignal(`menu-${++menuSeq}`);
  const popId = () => `${id()}-pop`;
  function hide() {
    document.getElementById(popId())?.hidePopover();
  }
  return (
    <>
      <button
        id={`${id()}-btn`}
        class="btn btn-outline btn-sm btn-icon"
        type="button"
        aria-label="Actions"
        popovertarget={popId()}
      >
        <EllipsisIcon />
      </button>
      <div id={popId()} popover="auto" class="popover-menu action-popover">
        <For each={props.actions}>
          {(a) => (
            <button
              class={`btn ${a.danger ? "btn-decline" : ""}`}
              type="button"
              onClick={() => {
                hide();
                a.onClick();
              }}
            >
              {a.label}
            </button>
          )}
        </For>
      </div>
    </>
  );
}

export function RowActions(props: {
  holdId: string;
  onApprove: () => void;
  onDecline: () => void;
}) {
  const popId = `actions-${props.holdId}`;
  function hide() {
    document.getElementById(popId)?.hidePopover();
  }
  return (
    <>
      <button
        id={`abtn-${props.holdId}`}
        class="btn btn-outline btn-sm btn-icon"
        type="button"
        aria-label="Actions"
        popovertarget={popId}
      >
        <EllipsisIcon />
      </button>
      <div id={popId} popover="auto" class="popover-menu action-popover">
        <button
          class="btn btn-approve"
          type="button"
          onClick={() => {
            hide();
            props.onApprove();
          }}
        >
          <CheckIcon /> Approve
        </button>
        <button
          class="btn btn-decline"
          type="button"
          onClick={() => {
            hide();
            props.onDecline();
          }}
        >
          <XIcon /> Decline
        </button>
      </div>
    </>
  );
}

export function FileViewer(props: { content: string }) {
  const lines = () => props.content.replace(/\n$/, "").split("\n");
  return (
    <div class="file-content">
      <pre>
        <For each={lines()}>
          {(line, i) => (
            <div>
              <span class="line-num">{i() + 1}</span>
              {line || "\u00a0"}
            </div>
          )}
        </For>
      </pre>
    </div>
  );
}

export function HoldTable(props: { holds: Hold[] }) {
  return (
    <div class="table-wrap">
      <table class="responsive">
        <thead>
          <tr>
            <th>Status</th>
            <th>Account</th>
            <th>Amount</th>
            <th>RDFI</th>
            <th>Customer</th>
          </tr>
        </thead>
        <tbody>
          <For each={props.holds}>
            {(h) => (
              <tr>
                <td data-label="Status">
                  <Badge status={h.status} />
                </td>
                <td data-label="Account">
                  <A href={`/holds/${h.id}`} class="file-link">
                    {h.entry_receiver_account}
                  </A>
                  <Show when={h.entry_receiver_name}>
                    <span class="cell-sub">{h.entry_receiver_name}</span>
                  </Show>
                </td>
                <td data-label="Amount" class="amount">
                  {dollars(h.entry_amount)}
                </td>
                <td data-label="RDFI">{h.entry_rdfi}</td>
                <td data-label="Customer">
                  <span class="muted truncate">{h.customer_id || "—"}</span>
                  <Show when={h.company_name}>
                    <span class="cell-sub">{h.company_name}</span>
                  </Show>
                </td>
              </tr>
            )}
          </For>
        </tbody>
      </table>
    </div>
  );
}

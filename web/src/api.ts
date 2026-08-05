export interface Hold {
  id: string;
  submission_id: string;
  entry_id: string;
  status: string;
  reason: string;
  release_artifact_id?: string;
  entry_trace: string;
  entry_rdfi: string;
  entry_receiver_name: string;
  entry_receiver_account: string;
  entry_amount: number;
  entry_tran_code: number;
  effective_date?: string;
  created_at: string;
  customer_id: string;
  filename: string;
}

export interface Review {
  id: string;
  hold_id: string;
  actor: string;
  action: string;
  note: string;
  created_at: string;
}

export interface VelocityInfo {
  total: number;
  prior: number;
  held: number;
}

export interface HoldDetail extends Hold {
  reviews: Review[];
  velocity?: VelocityInfo;
  release_state: string;
  group_size: number;
}

export interface Submission {
  id: string;
  filename: string;
  source_checksum: string;
  status: string;
  failed_reason: string;
  received_at: string;
}

export interface Artifact {
  id: string;
  submission_id: string;
  kind: string;
  checksum: string;
  state: string;
}

export interface Job {
  id: string;
  kind: string;
  ref: string;
  state: string;
  failures: number;
  last_error: string;
  run_at: string;
}

export interface SubmissionDetail extends Submission {
  artifacts: Artifact[];
  holds: Hold[];
  jobs: Job[];
}

export interface BatchEntry {
  id: string;
  header_id: string;
  rdfi: string;
  receiver_name: string;
  receiver_account: string;
  amount: number;
  tran_code: number;
  trace: string;
  effective_date?: string;
  customer_id: string;
  filename: string;
}

export interface BatchHeader {
  id: string;
  submission_id: string;
  customer_id: string;
  company_name: string;
  company_description: string;
  effective_date?: string;
  filename: string;
}

export interface EventRecord {
  id: string;
  type: string;
  ref?: string;
  payload?: unknown;
  created_at: string;
}

export interface Dashboard {
  hold_counts: Record<string, number>;
  pending: Hold[];
  failed_submissions: Submission[];
  failed_jobs: FailedJob[];
  velocity_leaks: VelocityLeak[];
}

export interface FailedJob {
  id: string;
  submission_id?: string;
  kind: string;
  filename: string;
  last_error: string;
}

export interface VelocityLeak {
  account: string;
  total: number;
  prior: number;
  held: number;
  effective_date: string;
  filename?: string;
}

export interface HoldsPage {
  holds: Hold[];
  total: number;
}

export interface EntriesPage {
  entries: BatchEntry[];
  total: number;
}

export interface HeadersPage {
  headers: BatchHeader[];
  total: number;
}

export interface EventsPage {
  events: EventRecord[];
  total: number;
}

export interface ArtifactSum {
  present: boolean;
  entries: number;
  total: number;
  error?: string;
}

export interface EntryMatch {
  trace: string;
  amount: number;
  original_account: string;
  fixed_account: string;
  cleaned_account: string;
  receiver_kept: boolean;
}

export interface SubmissionVerify {
  verified: boolean;
  artifacts: Record<string, ArtifactSum>;
  issues: string[];
  entries: EntryMatch[];
}

export interface ListParams {
  status?: string;
  q?: string;
  start_date?: string;
  end_date?: string;
  page?: number;
  pageSize?: number;
  sort?: string;
  dir?: string;
}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  if (!res.ok) {
    let msg = res.statusText;
    try {
      const body = await res.json();
      if (body?.error) msg = body.error;
    } catch {
      /* not json */
    }
    throw new Error(msg);
  }
  return res.json() as Promise<T>;
}

function qs(p: ListParams): string {
  const parts: string[] = [];
  if (p.status) parts.push(`status=${encodeURIComponent(p.status)}`);
  if (p.q) parts.push(`q=${encodeURIComponent(p.q)}`);
  if (p.start_date) parts.push(`start_date=${p.start_date}`);
  if (p.end_date) parts.push(`end_date=${p.end_date}`);
  if (p.page) parts.push(`page=${p.page}`);
  if (p.pageSize) parts.push(`pageSize=${p.pageSize}`);
  if (p.sort) parts.push(`sort=${p.sort}`);
  if (p.dir) parts.push(`dir=${p.dir}`);
  return parts.length ? "?" + parts.join("&") : "";
}

export const api = {
  dashboard: () => req<Dashboard>("/api/dashboard"),
  holds: (p: ListParams = {}) => req<HoldsPage>(`/api/holds${qs(p)}`),
  hold: (id: string) => req<HoldDetail>(`/api/holds/${id}`),
  approve: (id: string, body: { note: string; actor: string }) =>
    req(`/api/holds/${id}/approve`, { method: "POST", body: JSON.stringify(body) }),
  decline: (id: string, body: { note: string; actor: string }) =>
    req(`/api/holds/${id}/decline`, { method: "POST", body: JSON.stringify(body) }),
  bulk: (action: "approve" | "decline", ids: string[], body: { note: string; actor: string }) =>
    req<{ count: number }>(`/api/holds/bulk`, {
      method: "POST",
      body: JSON.stringify({ action, ids, note: body.note, actor: body.actor }),
    }),
  submissions: (status?: string) =>
    req<Submission[]>(`/api/submissions${status ? `?status=${status}` : ""}`),
  submission: (id: string) => req<SubmissionDetail>(`/api/submissions/${id}`),
  verifySubmission: (id: string) => req<SubmissionVerify>(`/api/submissions/${id}/verify`),
  entries: (p: ListParams = {}) => req<EntriesPage>(`/api/entries${qs(p)}`),
  headers: (p: ListParams = {}) => req<HeadersPage>(`/api/headers${qs(p)}`),
  artifactContent: (id: string) =>
    fetch(`/api/artifacts/${id}/content`).then((r) => {
      if (!r.ok) throw new Error("failed to load artifact");
      return r.text();
    }),
  events: (p: ListParams = {}) => req<EventsPage>(`/api/events${qs(p)}`),
};

export function dollars(cents: number): string {
  return "$" + (cents / 100).toFixed(2);
}

const LABELS: Record<string, string> = {
  pending: "Pending",
  approved: "Approved",
  declined: "Declined",
  auto_declined: "Auto declined",
  received: "Received",
  ready: "Ready",
  failed: "Failed",
  staged: "Staged",
  published: "Published",
  archived: "Archived",
  pruned: "Pruned",
  queued: "Queued",
  in_progress: "In progress",
  done: "Done",
  verified: "Verified",
  pass: "OK",
  blocked: "Blocked",
  awaiting_approval: "Awaiting approval",
  release: "Release",
  cleaned: "Cleaned",
  original: "Original",
  fixed: "Fixed",
};

// human renders an internal snake_case status/state as plain English.
export function human(s: string): string {
  if (LABELS[s]) return LABELS[s];
  return s.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
}

// fmtDateTime renders an ISO timestamp in a readable local format.
export function fmtDateTime(iso?: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

// fmtDate renders an ISO date (timestamp or YYYY-MM-DD) as a short local date.
export function fmtDate(iso?: string): string {
  if (!iso) return "—";
  // Date-only values ("YYYY-MM-DD") are parsed as local, not UTC midnight,
  // so they don't shift a day in negative time zones.
  const m = iso.match(/^(\d{4})-(\d{2})-(\d{2})$/);
  const d = m
    ? new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
    : new Date(iso);
  if (isNaN(d.getTime())) return iso;
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}

// releaseStateLabel explains what a hold's release status means.
export function releaseStateLabel(state: string): string {
  switch (state) {
    case "none":
      return "No release planned";
    case "published":
      return "Released";
    case "blocked":
      return "Blocked — a hold in this group was declined";
    case "awaiting_approval":
      return "Awaiting approval of every hold in this group";
    default:
      return human(state);
  }
}

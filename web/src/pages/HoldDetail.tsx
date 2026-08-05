import { createResource, createSignal, For, Show } from "solid-js";
import { useParams, A } from "@solidjs/router";
import { api, dollars, fmtDateTime, fmtDate, releaseStateLabel } from "../api";
import { Badge, Loading } from "../components";
import { useFlash } from "../flash";
import { useConfirm } from "../confirm";
import { canReview } from "../user";
import { ArrowLeftIcon, CheckIcon, XIcon } from "../icons";

export default function HoldDetail() {
  const params = useParams<{ id: string }>();
  const { show: flash } = useFlash();
  const { confirm } = useConfirm();
  const [hold, { refetch }] = createResource(() => params.id, (id) => api.hold(id));
  const [reviewer, setReviewer] = createSignal(localStorage.getItem("ach.actor") ?? "");
  const [note, setNote] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  async function act(action: "approve" | "decline") {
    setBusy(true);
    localStorage.setItem("ach.actor", reviewer());
    try {
      if (action === "approve") await api.approve(params.id, { note: note(), actor: reviewer() });
      else await api.decline(params.id, { note: note(), actor: reviewer() });
      flash("success", `${action === "approve" ? "Approved" : "Declined"}`);
      refetch();
    } catch (e) {
      flash("error", String(e));
    }
    setBusy(false);
  }

  return (
    <Show when={hold()} fallback={<Loading label="Loading hold…" />}>
      {(h) => (
        <>
          <A class="back-link" href="/holds">
            <ArrowLeftIcon /> Holds
          </A>
          <h1>
            Hold on {h().entry_receiver_account} <Badge status={h().status} />
          </h1>

          <div class="detail-card">
            <h2>Entry</h2>
            <div class="detail-grid">
              <div class="key">Status</div>
              <div class="val">{h().status}</div>
              <div class="key">Why held</div>
              <div class="val">{h().reason || "—"}</div>
              <div class="key">Receiver</div>
              <div class="val">{h().entry_receiver_name || "—"}</div>
              <div class="key">Account</div>
              <div class="val">{h().entry_receiver_account}</div>
              <div class="key">RDFI</div>
              <div class="val">{h().entry_rdfi}</div>
              <div class="key">Amount</div>
              <div class="val">{dollars(h().entry_amount)}</div>
              <div class="key">Trace</div>
              <div class="val">{h().entry_trace}</div>
              <div class="key">Effective date</div>
              <div class="val">{fmtDate(h().effective_date)}</div>
              <div class="key">Source file</div>
              <div class="val">
                <A href={`/submissions/${h().submission_id}`} class="file-link">
                  {h().filename}
                </A>
              </div>
              <div class="key">Release</div>
              <div class="val">{releaseStateLabel(h().release_state)}</div>
              <div class="key">Created</div>
              <div class="val">{fmtDateTime(h().created_at)}</div>
            </div>
          </div>

          <Show when={h().velocity}>
            {(v) => (
              <div class="detail-card">
                <h2>Same-day velocity</h2>
                <p class="section-note">
                  Total funds this account received today across all files, and how much already
                  shipped before this hold tripped.
                </p>
                <div class="detail-grid">
                  <div class="key">Account</div>
                  <div class="val">{h().entry_receiver_account}</div>
                  <div class="key">Same-day total</div>
                  <div class="val">{dollars(v().total)}</div>
                  <div class="key">Already shipped before this hold</div>
                  <div class="val">
                    <Show when={v().prior > 0} fallback={<span class="muted">—</span>}>
                      <span class="leak">{dollars(v().prior)}</span>
                    </Show>
                  </div>
                  <div class="key">Held by this hold</div>
                  <div class="val">{dollars(v().held)}</div>
                </div>
              </div>
            )}
          </Show>

          <Show when={canReview() && (h().status === "pending" || h().status === "auto_declined")}>
            <div class="detail-card">
              <h2>Review</h2>
              <Show when={h().group_size > 1}>
                <p class="section-note">
                  This hold is part of a velocity group of {h().group_size}. Approving or
                  declining it applies to the whole group.
                </p>
              </Show>
              <div class="search-bar">
                <form
                  onSubmit={(e) => {
                    e.preventDefault();
                    act("approve");
                  }}
                >
                  <label>
                    Reviewer{" "}
                    <input
                      type="text"
                      value={reviewer()}
                      placeholder="Your name"
                      onInput={(e) => setReviewer(e.currentTarget.value)}
                    />
                  </label>
                  <label>
                    Note{" "}
                    <input
                      type="text"
                      value={note()}
                      placeholder="Optional note"
                      onInput={(e) => setNote(e.currentTarget.value)}
                    />
                  </label>
                  <button class="btn btn-approve" type="submit" disabled={busy()}>
                    <CheckIcon /> Approve
                  </button>
                  <button
                    class="btn btn-decline"
                    type="button"
                    disabled={busy()}
                    onClick={() => act("decline")}
                  >
                    <XIcon /> Decline
                  </button>
                </form>
              </div>
            </div>
          </Show>

          <Show when={h().reviews.length > 0}>
            <h2 class="section">Review history</h2>
            <div class="table-wrap">
              <table class="responsive">
                <thead>
                  <tr>
                    <th>Action</th>
                    <th>Actor</th>
                    <th>Note</th>
                    <th>When</th>
                  </tr>
                </thead>
                <tbody>
                  <For each={h().reviews}>
                    {(r) => (
                      <tr>
                        <td data-label="Action">{r.action}</td>
                        <td data-label="Actor">{r.actor}</td>
                        <td data-label="Note">{r.note || "—"}</td>
                        <td data-label="When" class="muted">{fmtDateTime(r.created_at)}</td>
                      </tr>
                    )}
                  </For>
                </tbody>
              </table>
            </div>
          </Show>
        </>
      )}
    </Show>
  );
}

import { createSignal, Show } from "solid-js";
import { createAsync, useParams, useAction, useSubmission, A, revalidate, useNavigate } from "@solidjs/router";
import { dollars, fmtDate, human } from "../api";
import { Loading, QueryError, Select, Badge } from "../components";
import { useFlash } from "../flash";
import { useConfirm } from "../confirm";
import { canFlagEntry } from "../user";
import { entryQuery, createEntryHoldAction, usePolling } from "../queries";
import { ArrowLeftIcon } from "../icons";

const STATUSES = [
  { value: "", label: "Hold Status" },
  { value: "approved", label: "Approved" },
  { value: "declined", label: "Declined" },
];

export default function EntryDetail() {
  usePolling("entry");
  const params = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { show: flash } = useFlash();
  const { confirm } = useConfirm();
  const [error, setError] = createSignal<unknown>();
  const entry = createAsync(() => {
    setError(undefined);
    return entryQuery(params.id).catch((e) => {
      setError(e);
      return undefined;
    });
  });
  const createHold = useAction(createEntryHoldAction);
  const createHoldSub = useSubmission(createEntryHoldAction);
  const [holdStatus, setHoldStatus] = createSignal("");
  const busy = () => createHoldSub.pending;

  async function submitHold() {
    const e = entry();
    if (!e) return;
    const status = holdStatus();
    if (!status) {
      flash("error", "Select a hold status");
      return;
    }
    const res = await confirm(
      `Create a ${human(status)} hold on this entry?\n\nAccount: ${e.receiver_account}\nAmount: ${dollars(e.amount)}\nRDFI: ${e.rdfi}\nTrace: ${e.trace}\n\nThis will not modify any transmitted file; it only affects how future files to this receiver are screened.`,
      {
        title: `Create hold: ${human(status)}`,
        tone: status === "approved" ? "approve" : "decline",
        confirmLabel: "Create hold",
        showNote: true,
      },
    );
    if (!res.ok) return;
    const result = await createHold({ id: params.id, status, reason: res.note || "manual hold" });
    if (!result.ok) {
      flash("error", result.error ?? "failed");
      return;
    }
    flash("success", `Created ${human(status)} hold for ${e.receiver_account}`);
    navigate(`/holds/${result.holdId}`);
  }

  return (
    <Show
      when={entry()}
      fallback={
        <Show when={error()} fallback={<Loading label="Loading entry…" />}>
          <QueryError error={error()} onRetry={() => revalidate("entry")} />
        </Show>
      }
    >
      {(e) => (
        <>
          <A class="back-link" href="/entries">
            <ArrowLeftIcon /> Entries
          </A>
          <h1>
            Entry {e().trace}
          </h1>

          <div class="detail-card">
            <h2>Entry</h2>
            <div class="detail-grid">
              <div class="key">Receiver</div>
              <div class="val">{e().receiver_name || "—"}</div>
              <div class="key">Account</div>
              <div class="val">{e().receiver_account}</div>
              <div class="key">RDFI</div>
              <div class="val">{e().rdfi}</div>
              <div class="key">Amount</div>
              <div class="val">{dollars(e().amount)}</div>
              <div class="key">Trace</div>
              <div class="val">{e().trace}</div>
              <div class="key">Effective date</div>
              <div class="val">{fmtDate(e().effective_date)}</div>
              <div class="key">Customer</div>
              <div class="val">{e().customer_id || "—"}</div>
              <div class="key">Source file</div>
              <div class="val">
                <A href={`/submissions/${e().submission_id}`} class="file-link">
                  {e().filename}
                </A>
              </div>
            </div>
          </div>

          <Show when={e().hold_id}>
            <div class="detail-card">
              <h2>Attached hold</h2>
              <div class="detail-grid">
                <div class="key">Status</div>
                <div class="val"><Badge status={e().hold_status!} /></div>
              </div>
              <div class="btn-group" style="margin-top: 12px">
                <A class="btn btn-outline" href={`/holds/${e().hold_id}`}>
                  View hold
                </A>
              </div>
            </div>
          </Show>

          <Show when={!e().hold_id && canFlagEntry()}>
            <div class="detail-card">
              <h2>Create hold</h2>
              <p class="section-note">
                Manually flag this entry for review. This will not modify any
                transmitted file — it only affects how future files to this
                receiver are screened (via the latest-updated combo standing).
              </p>
              <div class="btn-group">
                <Select
                  value={holdStatus()}
                  options={STATUSES}
                  onChange={setHoldStatus}
                />
                <button
                  class="btn btn-approve"
                  type="button"
                  disabled={busy() || !holdStatus()}
                  onClick={submitHold}
                >
                  Create hold
                </button>
              </div>
            </div>
          </Show>
        </>
      )}
    </Show>
  );
}

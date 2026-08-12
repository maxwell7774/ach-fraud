import { createMemo, createSignal, For, Show } from "solid-js";
import { createAsync, useAction } from "@solidjs/router";
import { recipientsQuery, createRecipientAction, updateRecipientAction, deleteRecipientAction } from "../queries";
import type { Recipient } from "../api";
import { EmptyState, Loading, RowMenu } from "../components";
import { useFlash } from "../flash";
import { useConfirm } from "../confirm";

const ALERT_OPTIONS = [
  { value: "pending_holds", label: "New holds" },
  { value: "velocity_leaks", label: "Velocity leaks" },
  { value: "release_blocked", label: "Blocked releases" },
  { value: "failed", label: "Failures" },
];

function blankForm() {
  return { email: "", name: "", enabled: true, alert_types: [] as string[] };
}

export default function Recipients() {
  const { show: flash } = useFlash();
  const { confirm } = useConfirm();
  const create = useAction(createRecipientAction);
  const update = useAction(updateRecipientAction);
  const remove = useAction(deleteRecipientAction);
  const data = createAsync(() => recipientsQuery());

  const [editing, setEditing] = createSignal<string | null>(null);
  const [form, setForm] = createSignal(blankForm());

  function startEdit(r: Recipient) {
    setEditing(r.id);
    setForm({ email: r.email, name: r.name, enabled: r.enabled, alert_types: [...r.alert_types] });
  }
  function cancelEdit() {
    setEditing(null);
    setForm(blankForm());
  }
  function toggleAlert(t: string) {
    const set = new Set(form().alert_types);
    if (set.has(t)) set.delete(t);
    else set.add(t);
    setForm((f) => ({ ...f, alert_types: [...set] }));
  }

  async function save() {
    if (!form().email.trim()) {
      flash("error", "email is required");
      return;
    }
    const action = editing() ? update : create;
    const res = await action({ id: editing()!, ...form() });
    if (!res.ok) {
      flash("error", res.error ?? "failed to save recipient");
      return;
    }
    flash("success", editing() ? "Recipient updated" : "Recipient added");
    cancelEdit();
  }

  async function del(r: Recipient) {
    if (!(await confirm(`Delete recipient ${r.email}? They will stop receiving email alerts.`, { title: "Delete recipient" })))
      return;
    const res = await remove({ id: r.id });
    if (!res.ok) {
      flash("error", res.error ?? "failed to delete recipient");
      return;
    }
    flash("success", "Recipient deleted");
  }

  const rows = createMemo(() => data()?.recipients ?? []);

  return (
    <>
      <h1>Email recipients</h1>
      <p class="section-note">
        Who receives the run digest and which alert categories each person gets. The digest is one email
        per pipeline run, sent only when something alert-worthy happened.
      </p>

      <div class="detail-card">
        <h2>{editing() ? "Edit recipient" : "Add recipient"}</h2>
        <form
          class="recipient-form"
          onSubmit={(e) => {
            e.preventDefault();
            save();
          }}
        >
          <label>
            Email
            <input
              type="email"
              required
              placeholder="ops@example.com"
              value={form().email}
              onInput={(e) => setForm((f) => ({ ...f, email: e.currentTarget.value }))}
            />
          </label>
          <label>
            Name
            <input
              type="text"
              placeholder="Optional"
              value={form().name}
              onInput={(e) => setForm((f) => ({ ...f, name: e.currentTarget.value }))}
            />
          </label>
          <label class="check">
            <input
              type="checkbox"
              checked={form().enabled}
              onChange={(e) => setForm((f) => ({ ...f, enabled: e.currentTarget.checked }))}
            />
            Enabled
          </label>
          <div class="alert-options">
            <For each={ALERT_OPTIONS}>
              {(o) => (
                <label class="check">
                  <input
                    type="checkbox"
                    checked={form().alert_types.includes(o.value)}
                    onChange={() => toggleAlert(o.value)}
                  />
                  {o.label}
                </label>
              )}
            </For>
          </div>
          <div class="btn-group">
            <button class="btn btn-approve" type="submit">
              {editing() ? "Save changes" : "Add recipient"}
            </button>
            <Show when={editing()}>
              <button class="btn btn-outline" type="button" onClick={cancelEdit}>
                Cancel
              </button>
            </Show>
          </div>
        </form>
      </div>

      <Show when={data()} fallback={<Loading label="Loading recipients…" />}>
        <div class="table-wrap">
          <Show
            when={rows().length > 0}
            fallback={<EmptyState message="No recipients yet. Add one above to start receiving alert emails." />}
          >
            <table class="responsive">
              <thead>
                <tr>
                  <th>Email</th>
                  <th>Name</th>
                  <th>Enabled</th>
                  <th>Alerts</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                <For each={rows()}>
                  {(r) => (
                    <tr>
                      <td data-label="Email">{r.email}</td>
                      <td data-label="Name" class="muted">{r.name || "—"}</td>
                      <td data-label="Enabled">
                        <span class={r.enabled ? "muted" : "muted"}>
                          {r.enabled ? "Yes" : "No"}
                        </span>
                      </td>
                      <td data-label="Alerts">
                        <Show when={r.alert_types.length > 0} fallback={<span class="muted">none</span>}>
                          <span class="alert-badges">
                            <For each={r.alert_types}>
                              {(t) => (
                                <span class="badge badge-group">
                                  {ALERT_OPTIONS.find((o) => o.value === t)?.label ?? t}
                                </span>
                              )}
                            </For>
                          </span>
                        </Show>
                      </td>
                      <td data-label="Actions">
                        <RowMenu
                          actions={[
                            { label: "Edit", onClick: () => startEdit(r) },
                            { label: "Delete", onClick: () => del(r), danger: true },
                          ]}
                        />
                      </td>
                    </tr>
                  )}
                </For>
              </tbody>
            </table>
          </Show>
        </div>
      </Show>
    </>
  );
}

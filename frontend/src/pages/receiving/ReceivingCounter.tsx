import { UrgencyCounter } from "../../components";
import { BATCH_STATES } from "../../lib/urgency";
import { useReceiving } from "./useReceiving";

/** Счётчик неподтверждённых сроков в шапке; клик открывает приёмку. Нуль — ничего не показывает. */
export function ReceivingCounter() {
  const [state] = useReceiving();
  if (state.kind !== "ready" || state.rows.length === 0) return null;
  return (
    <span data-testid="receiving-counter" aria-label={`Неподтверждённых сроков: ${state.rows.length}`}>
      <UrgencyCounter def={BATCH_STATES.expiring} n={state.rows.length} text="без подтверждённого срока" href="#/receiving" />
    </span>
  );
}

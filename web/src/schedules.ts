import type { Rule, Schedule, ServiceWindow } from "./api";

const DAY_LABELS = ["Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"];

// describeSchedule reads one schedule back in the operator's words, so a
// window row and a schedule row say the same thing about the same clock.
export function describeSchedule(schedule: Schedule | undefined): string {
   if (!schedule) {
      return "";
   }
   return schedule.windows
      .map((window) => {
         const days = window.days.length === 7 ? "every day" : window.days.map((day) => DAY_LABELS[day] ?? "?").join(" ");
         return `${days} ${window.start}-${window.end}`;
      })
      .join("; ");
}

// scheduleUsage counts what still names a schedule, so the list can explain a
// refused delete before the operator presses it.
export function scheduleUsage(name: string, windows: ServiceWindow[], rules: Rule[]): { windows: number; rules: number } {
   return {
      windows: windows.filter((window) => window.schedule === name).length,
      rules: rules.filter((rule) => rule.schedule === name).length,
   };
}

import { describe, expect, it } from "vitest";
import { describeSchedule, scheduleUsage } from "./schedules";
import type { Rule, Schedule, ServiceWindow } from "./api";

const morning: Schedule = {
  name: "morning",
  priority: 2,
  windows: [
    { days: [1, 2, 3, 4, 5], start: "05:00", end: "07:00" },
    { days: [0, 6], start: "09:00", end: "11:00" },
  ],
};

describe("describeSchedule", () => {
  it("lists every window with its days and span", () => {
    expect(describeSchedule(morning)).toBe("Mo Tu We Th Fr 05:00-07:00; Su Sa 09:00-11:00");
  });

  it("says every day when the windows cover the week", () => {
    const week: Schedule = { name: "s", priority: 1, windows: [{ days: [0, 1, 2, 3, 4, 5, 6], start: "00:00", end: "24:00" }] };
    expect(describeSchedule(week)).toBe("every day 00:00-24:00");
  });

  it("answers empty for a missing schedule", () => {
    expect(describeSchedule(undefined)).toBe("");
  });
});

describe("scheduleUsage", () => {
  const windows: ServiceWindow[] = [
    { name: "a", action: "block", schedule: "morning", clients: ["d"], services: ["netflix"] },
    { name: "b", action: "allow", schedule: "morning", clients: ["d"], services: ["spotify"] },
    { name: "c", action: "block", schedule: "night", clients: ["d"], services: ["netflix"] },
  ];
  const rules: Rule[] = [
    { id: 1, domain: "x.com", kind: "subdomains", action: "block", schedule: "morning" },
    { id: 2, domain: "y.com", kind: "subdomains", action: "block" },
  ];

  it("counts the windows and rules that name the schedule", () => {
    expect(scheduleUsage("morning", windows, rules)).toEqual({ windows: 2, rules: 1 });
  });

  it("answers zeros for an unused schedule", () => {
    expect(scheduleUsage("holiday", windows, rules)).toEqual({ windows: 0, rules: 0 });
  });
});

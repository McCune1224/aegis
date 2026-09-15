import type { JSX } from "@solidjs/web";

function base(children: JSX.Element): JSX.Element {
  return (
    <svg
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      stroke-width="1.4"
      stroke-linecap="round"
      stroke-linejoin="round"
    >
      {children}
    </svg>
  );
}

export const IconGraph = () =>
  base(
    <>
      <circle cx="3" cy="13" r="1.2" />
      <circle cx="8" cy="8" r="1.2" />
      <circle cx="13" cy="3" r="1.2" />
      <path d="M4 12 L7 9 M9 7 L12 4" />
    </>,
  );

export const IconDashboard = () =>
  base(
    <>
      <rect x="2" y="2" width="5" height="5" rx="1" />
      <rect x="9" y="2" width="5" height="9" rx="1" />
      <rect x="2" y="9" width="5" height="5" rx="1" />
      <rect x="9" y="13" width="5" height="1.5" rx="0.75" />
    </>,
  );

export const IconLog = () =>
  base(
    <>
      <path d="M3 3h10 M3 6.5h10 M3 10h7 M3 13.5h4" />
    </>,
  );

export const IconClients = () =>
  base(
    <>
      <circle cx="6" cy="6" r="2.4" />
      <path d="M2.5 13.5c0-2 1.6-3.4 3.5-3.4s3.5 1.4 3.5 3.4" />
      <circle cx="11.5" cy="6.5" r="1.9" />
      <path d="M11 10.4c1.7 0 2.9 1.2 2.9 3.1" />
    </>,
  );

export const IconShield = () =>
  base(
    <>
      <path d="M8 1.8 L13.2 3.6 V8 c0 3.2-2.2 5.2-5.2 6.3 C5 13.2 2.8 11.2 2.8 8 V3.6 Z" />
      <path d="M5.8 8 l1.6 1.6 3-3.2" />
    </>,
  );

export const IconRules = () =>
  base(
    <>
      <path d="M3 4h8 M3 8h10 M3 12h6" />
      <path d="M12.2 2.6 l1.4 1.4 -2.6 2.6 -1.6.2.2-1.6 Z" />
    </>,
  );

export const IconSources = () =>
  base(
    <>
      <ellipse cx="8" cy="3.8" rx="5.5" ry="2.2" />
      <path d="M2.5 3.8 v8.4 c0 1.2 2.5 2.2 5.5 2.2 s5.5-1 5.5-2.2 V3.8" />
      <path d="M2.5 8 c0 1.2 2.5 2.2 5.5 2.2 s5.5-1 5.5-2.2" />
    </>,
  );

export const IconGear = () =>
  base(
    <>
      <circle cx="8" cy="8" r="2.4" />
      <path d="M8 1.6v2 M8 12.4v2 M1.6 8h2 M12.4 8h2 M3.5 3.5l1.4 1.4 M11.1 11.1l1.4 1.4 M12.5 3.5l-1.4 1.4 M4.9 11.1l-1.4 1.4" />
    </>,
  );

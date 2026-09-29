import type { JSX } from "@solidjs/web";

function base(children: JSX.Element): JSX.Element {
  return (
    <svg
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      stroke-width="1.5"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      {children}
    </svg>
  );
}

export const IconDashboard = () =>
  base(
    <>
      <rect x="2" y="2" width="5" height="5" rx="1" />
      <rect x="9" y="2" width="5" height="9" rx="1" />
      <rect x="2" y="9" width="5" height="5" rx="1" />
      <rect x="9" y="13" width="5" height="1.5" rx="0.75" />
    </>,
  );

export const IconConstellation = () =>
  base(
    <>
      <path d="M4 12.4 L8 7.2 L12.6 10 L13 3.6" />
      <circle cx="4" cy="12.4" r="1.3" fill="currentColor" stroke="none" />
      <circle cx="8" cy="7.2" r="1.5" fill="currentColor" stroke="none" />
      <circle cx="12.6" cy="10" r="1.1" fill="currentColor" stroke="none" />
      <circle cx="13" cy="3.6" r="1.7" fill="currentColor" stroke="none" />
    </>,
  );

export const IconLog = () =>
  base(
    <>
      <path d="M3 3.5h10 M3 7h10 M3 10.5h6.5 M3 14h4" />
    </>,
  );

export const IconClients = () =>
  base(
    <>
      <circle cx="6" cy="5.6" r="2.4" />
      <path d="M2.4 13.6c0-2 1.6-3.5 3.6-3.5s3.6 1.5 3.6 3.5" />
      <circle cx="11.6" cy="6.4" r="1.9" />
      <path d="M11.2 10.4c1.7.1 2.9 1.3 2.9 3.2" />
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

export const IconClock = () =>
  base(
    <>
      <circle cx="8" cy="8" r="6.2" />
      <path d="M8 4.6 V8 l2.4 1.6" />
    </>,
  );

export const IconServices = () =>
  base(
    <>
      <path d="M2.5 5h11 M2.5 11h11" />
      <circle cx="10.5" cy="5" r="2" />
      <circle cx="5.5" cy="11" r="2" />
    </>,
  );

export const IconRewrite = () =>
  base(
    <>
      <path d="M2.5 5.5 h9 M9.5 3 l2.5 2.5 -2.5 2.5" />
      <path d="M13.5 11.5 h-9 M6.5 9 l-2.5 2.5 2.5 2.5" />
    </>,
  );

export const IconUpstream = () =>
  base(
    <>
      <circle cx="5" cy="11" r="2.6" />
      <path d="M7 9 L12.5 3.5" />
      <path d="M8.5 3.5 h4 v4" />
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

export const IconSystem = () =>
  base(
    <>
      <path d="M8 1.8 L14 4.8 8 7.8 2 4.8 Z" />
      <path d="M2 8 L8 11 14 8" />
      <path d="M2 11.2 L8 14.2 14 11.2" />
    </>,
  );

export const IconSearch = () =>
  base(
    <>
      <circle cx="7" cy="7" r="4.4" />
      <path d="M10.4 10.4 L13.6 13.6" />
    </>,
  );

export const IconGear = () =>
  base(
    <>
      <circle cx="8" cy="8" r="2.4" />
      <path d="M8 1.6v2 M8 12.4v2 M1.6 8h2 M12.4 8h2 M3.5 3.5l1.4 1.4 M11.1 11.1l1.4 1.4 M12.5 3.5l-1.4 1.4 M4.9 11.1l-1.4 1.4" />
    </>,
  );

import type { JSX } from "solid-js";

export type IconProps = { class?: string };

function svg(props: IconProps, children: JSX.Element): JSX.Element {
  return (
    <svg
      class={props.class}
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      {children}
    </svg>
  );
}

export function SunIcon(props: IconProps): JSX.Element {
  return svg(props, (
    <>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2m0 16v2M4.9 4.9l1.4 1.4m11.4 11.4 1.4 1.4M2 12h2m16 0h2M4.9 19.1l1.4-1.4m11.4-11.4 1.4-1.4" />
    </>
  ));
}

export function MoonIcon(props: IconProps): JSX.Element {
  return svg(props, (
    <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
  ));
}

export function ChevronLeftIcon(props: IconProps): JSX.Element {
  return svg(props, (<polyline points="15 18 9 12 15 6" />));
}

export function ChevronRightIcon(props: IconProps): JSX.Element {
  return svg(props, (<polyline points="9 18 15 12 9 6" />));
}

export function ChevronDownIcon(props: IconProps): JSX.Element {
  return svg(props, (<polyline points="6 9 12 15 18 9" />));
}

export function EllipsisIcon(props: IconProps): JSX.Element {
  return svg(props, (
    <>
      <circle cx="5" cy="12" r="1" />
      <circle cx="12" cy="12" r="1" />
      <circle cx="19" cy="12" r="1" />
    </>
  ));
}

export function XIcon(props: IconProps): JSX.Element {
  return svg(props, (<path d="M18 6 6 18M6 6l12 12" />));
}

export function MenuIcon(props: IconProps): JSX.Element {
  return svg(props, (<path d="M3 6h18M3 12h18M3 18h18" />));
}

export function AlertIcon(props: IconProps): JSX.Element {
  return svg(props, (
    <>
      <path d="M7.86 2h8.28L22 7.86v8.28L16.14 22H7.86L2 16.14V7.86z" />
      <path d="M12 8v4M12 16h.01" />
    </>
  ));
}

export function ArrowLeftIcon(props: IconProps): JSX.Element {
  return svg(props, (<path d="M19 12H5M12 19l-7-7 7-7" />));
}

export function CheckIcon(props: IconProps): JSX.Element {
  return svg(props, (<polyline points="20 6 9 17 4 12" />));
}

export function CalendarIcon(props: IconProps): JSX.Element {
  return svg(props, (
    <>
      <rect x="3" y="4" width="18" height="18" rx="2" />
      <path d="M16 2v4M8 2v4M3 10h18" />
    </>
  ));
}

export function ChevronUpDownIcon(props: IconProps): JSX.Element {
  return svg(props, (<path d="m7 15 5 5 5-5M7 9l5-5 5 5" />));
}

export function ArrowUpIcon(props: IconProps): JSX.Element {
  return svg(props, (<path d="M12 19V5M5 12l7-7 7 7" />));
}

export function ArrowDownIcon(props: IconProps): JSX.Element {
  return svg(props, (<path d="M12 5v14M19 12l-7 7-7-7" />));
}

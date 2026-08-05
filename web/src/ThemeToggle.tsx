import { createSignal } from "solid-js";
import { MoonIcon, SunIcon } from "./icons";

export default function ThemeToggle() {
  const [dark, setDark] = createSignal(
    document.documentElement.getAttribute("data-theme") === "dark"
  );
  function toggle() {
    const next = !dark();
    setDark(next);
    document.documentElement.setAttribute("data-theme", next ? "dark" : "light");
    localStorage.setItem("theme", next ? "dark" : "light");
  }
  return (
    <button class="theme-toggle" onClick={toggle} aria-label="Toggle theme">
      <span class="moon-icon">
        <MoonIcon />
      </span>
      <span class="sun-icon">
        <SunIcon />
      </span>
    </button>
  );
}

import { useEffect, useRef } from "react";

const dialogs: HTMLElement[] = [];
let savedOverflow = "";
let savedPadding = "";

/** Shared focus containment and scroll ownership, including nested dialogs. */
export function useDialog(open: boolean, onClose: () => void, dismissible = true) {
  const panel = useRef<HTMLDivElement>(null);
  const close = useRef(onClose);
  useEffect(() => { close.current = onClose; }, [onClose]);

  useEffect(() => {
    const element = panel.current;
    if (!open || !element) return;
    const trigger = document.activeElement;
    if (dialogs.length === 0) {
      savedOverflow = document.body.style.overflow;
      savedPadding = document.body.style.paddingRight;
      const gutter = document.documentElement.clientWidth > 0
        ? window.innerWidth - document.documentElement.clientWidth : 0;
      if (gutter > 0) {
        document.body.style.paddingRight = `${parseFloat(getComputedStyle(document.body).paddingRight) + gutter}px`;
      }
      document.body.style.overflow = "hidden";
    }
    dialogs.push(element);
    element.focus();
    const isTop = () => dialogs.at(-1) === element;
    const containFocus = (event: FocusEvent) => {
      if (isTop() && event.target instanceof Node && !element.contains(event.target)) element.focus();
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (!isTop()) return;
      if (event.key === "Escape" && dismissible) {
        event.preventDefault();
        event.stopPropagation();
        close.current();
      }
      if (event.key !== "Tab") return;
      const items = Array.from(element.querySelectorAll<HTMLElement>(
        'a[href], button, input:not([type="hidden"]), textarea, select, [tabindex]',
      )).filter(item => item.tabIndex >= 0 && !item.matches(":disabled") && !item.closest('[hidden], [inert], [aria-hidden="true"]') && getComputedStyle(item).display !== "none" && getComputedStyle(item).visibility !== "hidden");
      const first = items[0];
      const last = items.at(-1);
      if (!first || !last) {
        event.preventDefault();
        element.focus();
      } else if (event.shiftKey && (document.activeElement === first || document.activeElement === element)) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && (document.activeElement === last || document.activeElement === element)) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    document.addEventListener("focusin", containFocus);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      document.removeEventListener("focusin", containFocus);
      const index = dialogs.indexOf(element);
      if (index >= 0) dialogs.splice(index, 1);
      if (dialogs.length === 0) {
        document.body.style.overflow = savedOverflow;
        document.body.style.paddingRight = savedPadding;
      }
      if (trigger instanceof HTMLElement && trigger.isConnected) trigger.focus();
    };
  }, [open, dismissible]);
  return panel;
}

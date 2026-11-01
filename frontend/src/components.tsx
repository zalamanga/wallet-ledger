import { cloneElement, useEffect, useId, useRef, type ReactElement, type ReactNode } from "react";

export function Notice({ children, tone = "neutral" }: { children: ReactNode; tone?: "neutral" | "error" | "success" }) {
  return <div className={`notice ${tone}`} role={tone === "error" ? "alert" : "status"}>{children}</div>;
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="empty">{children}</p>;
}

export function Dialog({ title, children, onClose, busy = false, wide = false }: {
  title: string; children: ReactNode; onClose: () => void; busy?: boolean; wide?: boolean;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    const element = ref.current;
    const returnTo = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    element?.showModal();
    return () => {
      element?.close();
      if (returnTo?.isConnected) returnTo.focus();
    };
  }, []);
  return <dialog ref={ref} className={wide ? "dialog wide" : "dialog"} aria-labelledby={titleId}
    onCancel={(event) => { event.preventDefault(); if (!busy) closeRef.current(); }}>
    <div className="dialog-heading"><h2 id={titleId}>{title}</h2><button type="button" className="quiet" onClick={onClose} disabled={busy}>Tutup</button></div>
    {children}
  </dialog>;
}

export function Field({ label, children, hint }: {
  label: string;
  children: ReactElement<{ id?: string; "aria-describedby"?: string }>;
  hint?: string;
}) {
  const generatedId = useId();
  const controlId = children.props.id ?? generatedId;
  const hintId = `${generatedId}-hint`;
  const describedBy = [children.props["aria-describedby"], hint ? hintId : undefined].filter(Boolean).join(" ") || undefined;
  return <div className="field">
    <label htmlFor={controlId}>{label}</label>
    {cloneElement(children, { id: controlId, "aria-describedby": describedBy })}
    {hint && <small id={hintId}>{hint}</small>}
  </div>;
}

export function PageHeading({ title, description, extra }: { title: string; description?: string; extra?: ReactNode }) {
  return <div className="page-heading"><div><h1 id="page-heading">{title}</h1>{description && <p>{description}</p>}</div>{extra}</div>;
}

import { MoreHorizontal } from 'lucide-react';
import type { ReactNode } from 'react';
import { createContext, useContext, useEffect, useRef, useState } from 'react';

const CloseMenuContext = createContext<(() => void) | null>(null);

export function ContextMenu({
  label,
  children,
  align = 'right',
}: {
  label: string;
  children: ReactNode;
  align?: 'left' | 'right';
}) {
  const [open, setOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return undefined;
    const closeOnOutsidePointer = (event: PointerEvent) => {
      if (event.target instanceof Node && !menuRef.current?.contains(event.target)) {
        setOpen(false);
      }
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        setOpen(false);
      }
    };
    document.addEventListener('pointerdown', closeOnOutsidePointer);
    document.addEventListener('keydown', closeOnEscape);
    return () => {
      document.removeEventListener('pointerdown', closeOnOutsidePointer);
      document.removeEventListener('keydown', closeOnEscape);
    };
  }, [open]);

  return (
    <div className={`context-menu${open ? ' open' : ''}`} ref={menuRef}>
      <button
        type="button"
        className="context-menu-trigger"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        title={label}
        onClick={() => setOpen((current) => !current)}
      >
        <MoreHorizontal size={17} aria-hidden="true" />
      </button>
      {open && (
        <CloseMenuContext.Provider value={() => setOpen(false)}>
          <div className={`context-menu-list align-${align}`} role="menu" aria-label={label}>
            {children}
          </div>
        </CloseMenuContext.Provider>
      )}
    </div>
  );
}

export function ContextMenuItem({
  children,
  onClick,
  disabled = false,
  busy = false,
}: {
  children: ReactNode;
  onClick: () => void;
  disabled?: boolean;
  busy?: boolean;
}) {
  const closeMenu = useContext(CloseMenuContext);
  return (
    <button
      type="button"
      role="menuitem"
      disabled={disabled}
      aria-busy={busy}
      onClick={() => {
        onClick();
        closeMenu?.();
      }}
    >
      {children}
    </button>
  );
}

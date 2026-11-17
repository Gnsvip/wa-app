import type { ReactNode } from 'react';

export function WaUpdatesSection({ children, count, onOpenChange }: { children: ReactNode; count?: number; onOpenChange?: (open: boolean) => void }) {
  return (
    <details className="mt-3 border-t border-border pt-2" onToggle={(event) => onOpenChange?.(event.currentTarget.open)}>
      <summary className="cursor-pointer rounded-lg px-3 py-2 text-sm text-muted-foreground hover:bg-muted/70">频道与系统通知{count !== undefined ? `（${count}）` : ''}</summary>
      <p className="px-3 pb-2 text-xs text-muted-foreground">单独显示，不计入普通聊天未读。</p>
      {children}
    </details>
  );
}

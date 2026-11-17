import { useCallback, useEffect, useRef, useState } from 'react';
import { ListTree, RefreshCcw, Trash2 } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { getWaRuntimeLogs, type WaRuntimeLogEntry } from './wa-api';

const MAX_VISIBLE_LOGS = 160;
const POLL_INTERVAL_MS = 2500;

type Props = { className?: string };

export function WaRuntimeLogPanel({ className }: Props) {
  const [logs, setLogs] = useState<WaRuntimeLogEntry[]>([]);
  const [manualRefreshing, setManualRefreshing] = useState(false);
  const [autoScrollPaused, setAutoScrollPaused] = useState(false);
  const afterIDRef = useRef('');
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const autoScrollRef = useRef(true);
  const programmaticScrollRef = useRef(false);

  const fetchLogs = useCallback(async (manual = false) => {
    if (manual) setManualRefreshing(true);
    try {
      const response = await getWaRuntimeLogs(afterIDRef.current, 100);
      const incoming = response.logs || [];
      if (!incoming.length) return;
      afterIDRef.current = incoming[incoming.length - 1]?.id || afterIDRef.current;
      setLogs((current) => mergeLogs(current, incoming).slice(-MAX_VISIBLE_LOGS));
    } catch {
      // Keep the panel quiet during transient auth/network changes.
    } finally {
      if (manual) setManualRefreshing(false);
    }
  }, []);

  useEffect(() => {
    void fetchLogs();
    const timer = window.setInterval(() => void fetchLogs(), POLL_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, [fetchLogs]);

  const latestLogID = logs[logs.length - 1]?.id || '';

  const scrollLogsToBottom = useCallback(() => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    programmaticScrollRef.current = true;
    viewport.scrollTop = viewport.scrollHeight;
    autoScrollRef.current = true;
    setAutoScrollPaused(false);
    window.setTimeout(() => {
      programmaticScrollRef.current = false;
    }, 80);
  }, []);

  useEffect(() => {
    if (!autoScrollRef.current) return;
    const frame = window.requestAnimationFrame(scrollLogsToBottom);
    return () => window.cancelAnimationFrame(frame);
  }, [latestLogID, scrollLogsToBottom]);

  function handleViewportScroll() {
    if (programmaticScrollRef.current) return;
    const viewport = viewportRef.current;
    if (!viewport) return;
    const atBottom = isLogViewportNearBottom(viewport);
    autoScrollRef.current = atBottom;
    setAutoScrollPaused(!atBottom);
  }

  const latest = logs[logs.length - 1];
  const latestMessage = latest ? runtimeLogMessage(latest) : '等待新事件';

  return (
    <section
      className={cn(
        'grid h-[220px] overflow-hidden rounded-xl border border-border bg-card text-card-foreground',
        'grid-rows-[40px_1fr] shadow-none sm:h-[260px]',
        className
      )}
      aria-label="运行日志"
    >
      <header className="flex min-w-0 items-center justify-between gap-3 border-b bg-muted/20 px-4">
        <div className="flex min-w-0 items-center gap-2">
          <ListTree size={16} className="shrink-0" />
          <div className="min-w-0 truncate text-sm font-medium">
            运行日志
            <span className="font-normal text-muted-foreground"> · {latestMessage}</span>
            {autoScrollPaused ? <span className="font-normal text-muted-foreground"> · 查看历史中</span> : null}
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <Button type="button" size="icon" variant="ghost" className="size-8" title="刷新" aria-label="刷新日志" disabled={manualRefreshing} onClick={() => void fetchLogs(true)}>
            <RefreshCcw size={14} className={cn(manualRefreshing && 'animate-spin')} />
          </Button>
          <Button type="button" size="icon" variant="ghost" className="size-8" title="清空" aria-label="清空日志" onClick={() => {
            autoScrollRef.current = true;
            setAutoScrollPaused(false);
            setLogs([]);
          }}>
            <Trash2 size={14} />
          </Button>
        </div>
      </header>
      <div ref={viewportRef} className="thin-scrollbar min-h-0 overflow-y-auto bg-background/70 p-3" onScroll={handleViewportScroll}>
        {logs.length ? (
          <div className="grid gap-2">
            {logs.map((entry) => <RuntimeLogRow key={entry.id} entry={entry} />)}
          </div>
        ) : (
          <div className="grid h-full place-items-center text-sm text-muted-foreground">暂无日志</div>
        )}
      </div>
    </section>
  );
}

function isLogViewportNearBottom(viewport: HTMLElement) {
  return viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight < 32;
}

function RuntimeLogRow({ entry }: { entry: WaRuntimeLogEntry }) {
  const details = Object.entries(entry.details || {}).filter(([, value]) => value);
  return (
    <article className={cn('grid gap-1 rounded-md border p-2 text-xs', rowTone(entry.level))}>
      <div className="flex min-w-0 items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="break-words font-medium leading-5">{runtimeLogMessage(entry)}</div>
          <div className="mt-0.5 flex min-w-0 flex-wrap items-center gap-1 text-[11px] text-muted-foreground">
            <span>{formatLogTime(entry.at)}</span>
            <span>{runtimeLogSourceLabel(entry.source)}</span>
            <span>{runtimeLogStageLabel(entry.stage)}</span>
            {entry.correlation_id ? <span className="max-w-32 truncate">{entry.correlation_id}</span> : null}
          </div>
        </div>
        <Badge variant={entry.level === 'error' ? 'destructive' : entry.level === 'warn' ? 'secondary' : 'outline'} className="shrink-0 text-[10px]">
          {runtimeLogLevelLabel(entry.level)}
        </Badge>
      </div>
      {details.length ? (
        <dl className="mt-1 grid gap-1">
          {details.map(([key, value]) => (
            <div key={key} className="grid grid-cols-[88px_1fr] gap-2">
              <dt className="truncate text-muted-foreground">{runtimeLogDetailKeyLabel(key)}</dt>
              <dd className="min-w-0 break-words font-mono text-[11px] leading-5">{runtimeLogDetailValue(key, value)}</dd>
            </div>
          ))}
        </dl>
      ) : null}
    </article>
  );
}

function mergeLogs(current: WaRuntimeLogEntry[], incoming: WaRuntimeLogEntry[]) {
  const seen = new Set(current.map((entry) => entry.id));
  const merged = [...current];
  for (const entry of incoming) {
    if (!seen.has(entry.id)) {
      seen.add(entry.id);
      merged.push(entry);
    }
  }
  return merged;
}

function rowTone(level: WaRuntimeLogEntry['level']) {
  if (level === 'error') return 'border-destructive/45 bg-destructive/10';
  if (level === 'warn') return 'border-amber-400/45 bg-amber-50 text-amber-950 dark:bg-amber-950/20 dark:text-amber-100';
  return 'border-border bg-card';
}

function formatLogTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return date.toLocaleTimeString('zh-CN', { hour12: false });
}

function runtimeLogMessage(entry: WaRuntimeLogEntry) {
  const normalized = entry.message.trim();
  const detailError = entry.details?.error || '';
  if (entry.source === 'auto_sms_registration' && entry.stage === 'cancel' && entry.level === 'warn') {
    const reason = humanizeRuntimeError(detailError);
    return reason ? `自动接码取消接码订单失败：${reason}` : '自动接码取消接码订单失败';
  }
  return humanizeRuntimeMessage(normalized) || `${runtimeLogSourceLabel(entry.source)}${runtimeLogStageLabel(entry.stage)}`;
}

function runtimeLogLevelLabel(level: WaRuntimeLogEntry['level']) {
  if (level === 'error') return '错误';
  if (level === 'warn') return '警告';
  if (level === 'debug') return '调试';
  return '信息';
}

function runtimeLogSourceLabel(source: string) {
  const labels: Record<string, string> = {
    action_gateway: '前端操作',
    auto_sms_registration: '自动接码',
    registration_orchestrator: '注册流程',
    registration_orchestrator_legacy: '注册流程',
    registration: '注册流程',
    request_code: '请求验证码',
    sms_receiver: '接码平台',
  };
  return labels[source] || source;
}

function runtimeLogStageLabel(stage: string) {
  const labels: Record<string, string> = {
    allocate: '拿号',
    cancel: '取消订单',
    code_request: '请求验证码',
    detect: '检测',
    finish: '完成订单',
    probe: '检测',
    request_code: '请求验证码',
    receive_otp: '取码',
    register: '注册',
    registration: '注册',
    start: '开始',
    stop: '停止',
    submit_otp: '提交验证码',
    verification: '验证',
  };
  return labels[stage] || stage;
}

function runtimeLogDetailKeyLabel(key: string) {
  const labels: Record<string, string> = {
    attempt: '次数',
    accepted: '是否接受',
    cancel_error: '取消失败',
    countries: '国家',
    error: '错误',
    max_price: '最高价',
    method: '方式',
    order_id: '订单',
    phone_tail: '号码尾号',
    provider: '平台',
    raw_reason: '原始原因',
    raw_status: '原始状态',
    retry_after_seconds: '重试秒数',
    retry_after_sec: '重试秒数',
    service: '业务',
    skipped_count: '跳过',
    status: '状态',
    success_count: '成功',
    target: '目标',
    task_id: '任务',
    verification_request_id: '验证请求',
  };
  return labels[key] || key;
}

function runtimeLogDetailValue(key: string, value: string) {
  if (key === 'error' || key === 'cancel_error') return humanizeRuntimeError(value);
  if (key === 'method') return methodLabel(value);
  if (key === 'accepted') return boolLabel(value);
  if (key === 'raw_reason' || key === 'raw_status' || key === 'status') return statusValueLabel(value);
  return humanizeRuntimeValue(value);
}

function humanizeRuntimeError(value: string) {
  return value
    .replace(/^SMS receiver returned HTTP 500:\s*/i, '接码平台返回错误：')
    .replace(/^HTTP Error 409:\s*Conflict$/i, '订单状态冲突，平台已不允许取消')
    .replace(/HTTP Error 409:\s*Conflict/gi, '订单状态冲突，平台已不允许取消')
    .replace(/context deadline exceeded/gi, '请求超时')
    .replace(/Client\.Timeout exceeded while awaiting headers/gi, '等待接码平台响应超时')
    .replace(/WRONG_MAX_PRICE/gi, '价格超过最高价');
}

function humanizeRuntimeMessage(value: string) {
  return value
    .replace(/^WA 拒绝验证码请求$/i, 'WA 拒绝验证码请求')
    .replace(/^WA rejected verification request$/i, 'WA 拒绝验证码请求')
    .replace(/^开始 WA 注册流程$/i, '开始 WA 注册流程')
    .replace(/^Start WA registration flow$/i, '开始 WA 注册流程')
    .replace(/^自动接码价格超限，已取消订单并继续$/i, '自动接码价格超限，已取消订单并继续')
    .replace(/^auto sms registration/i, '自动接码');
}

function humanizeRuntimeValue(value: string) {
  if (/^(true|false)$/i.test(value)) return boolLabel(value);
  return value
    .replace(/verification request was rejected:\s*reason=blocked/gi, '验证码请求被拒绝：号码被封禁或受限')
    .replace(/VERIFICATION_REQUEST_STATUS_REJECTED/g, '验证码请求被拒绝')
    .replace(/\bblocked\b/gi, '已封禁/受限')
    .replace(/\bfail\b/gi, '失败')
    .replace(/\bsuccess\b/gi, '成功');
}

function methodLabel(value: string) {
  const labels: Record<string, string> = {
    sms: 'SMS',
    voice: '语音',
    email: '邮箱',
    wa_old: '旧设备',
    old_device: '旧设备',
    twofac_pin: '两步验证 PIN',
  };
  return labels[value.toLowerCase()] || value;
}

function boolLabel(value: string) {
  if (/^true$/i.test(value)) return '是';
  if (/^false$/i.test(value)) return '否';
  return value;
}

function statusValueLabel(value: string) {
  const normalized = value.trim().toLowerCase();
  const labels: Record<string, string> = {
    blocked: '已封禁/受限',
    fail: '失败',
    failed: '失败',
    rejected: '已拒绝',
    success: '成功',
    ok: '成功',
    verification_request_status_rejected: '验证码请求被拒绝',
  };
  return labels[normalized] || humanizeRuntimeValue(value);
}

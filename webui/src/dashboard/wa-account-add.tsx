import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { Check, CheckCircle2, ChevronDown, CircleDollarSign, CircleStop, Eraser, Loader2, Search, Settings, X } from 'lucide-react';
import { getCountries, getCountryCallingCode } from 'libphonenumber-js';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Separator } from '@/components/ui/separator';
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet';
import { useIsMobile } from '@/hooks/use-mobile';
import { cn } from '@/lib/utils';
import { allocateSmsReceiverOrder, cancelSmsReceiverOrder, finishSmsReceiverOrder, getAutoSmsRegistration, getSmsReceiverCountries, getSmsReceiverSettings, probeWaPhoneSMS, querySmsReceiverBalance, receiveWaRegistrationSMSOTP, registerWaPhone, saveSmsReceiverSettings, startAutoSmsRegistration, stopAutoSmsRegistration, submitWaRegistrationOTP, testSmsReceiverProxy, type AutoSmsRegistrationTask, type SmsReceiverAllocateInput, type SmsReceiverBalanceResult, type SmsReceiverCountry, type SmsReceiverOrder, type SmsReceiverProxyTestResult, type SmsReceiverSettings, type SmsReceiverSettingsUpdate, type WaWorkflowResponse } from './wa-api';
import { probeMatchesValues, registrationFailureMessage, workflowText, type WaAccountAddProbeState } from './wa-account-add-model';
import { WhatsAppIcon } from './wa-brand-icon';
import { accountReasonLabel, countdownLabel } from './wa-result-labels';
import { waProbeStatus } from './wa-result-model';
import { WaRegistrationChannelButtons } from './wa-registration-channel-buttons';
import { WaRegistrationOtpCard, WA_REGISTRATION_OTP_LENGTH } from './wa-registration-otp-card';
import {
  registrationAnyMethodAvailable,
  registrationChannelsHardBlocked,
  registrationMinimumCooldownSeconds,
  type SelectableRegistrationMethodOption,
} from './wa-registration-methods';
import { resolveInternationalWaPhone, resolveWaPhoneTarget, type WaResolvedPhone } from './wa-utils';
type PendingRegistration = { accountID: string; verificationRequestID: string; smsReceiverOrder?: SmsReceiverOrder };
type SmsProxyMode = 'config' | 'direct';
type Props = { className?: string; disabled?: boolean; footer?: ReactNode; onChanged: () => void | Promise<void>; onDone: (message: string) => void; onError: (message: string) => void };
const SMS_PROVIDER_OPTIONS = [
  { value: 'smsbower', label: 'smsbower' },
  { value: 'herosms', label: 'herosms' },
  { value: '5sim', label: '5sim' },
] as const;
type SmsProviderName = (typeof SMS_PROVIDER_OPTIONS)[number]['value'];
type SmsProviderOption = '' | SmsProviderName;
type SmsSettingsDraft = { proxy: string; providers: SmsProviderName[]; keys: Record<SmsProviderName, string>; clearKeys: Record<SmsProviderName, boolean> };
type SmsSettingsSaveState = 'idle' | 'saving' | 'saved' | 'error';
type SmsReceiverCountriesResult = Awaited<ReturnType<typeof getSmsReceiverCountries>>;
type ProxyTestState = { loading: boolean; direct?: SmsReceiverProxyTestResult; proxy?: SmsReceiverProxyTestResult; error?: string };
type SmsProviderBalanceState = { loading?: boolean; result?: SmsReceiverBalanceResult; error?: string };
const DEFAULT_SMS_COUNTRIES = 'country:colombia,country:chile,country:england,country:brazil,country:indonesia';
const DEFAULT_SMS_TARGET_COUNT = '0';
const SMS_COUNTRIES_STORAGE_KEY = 'wa.smsReceiver.countries.v1';
const SMS_TARGET_COUNT_STORAGE_KEY = 'wa.smsReceiver.targetCount.v1';
const countryNameFormatterZh = typeof Intl.DisplayNames === 'function' ? new Intl.DisplayNames(['zh-CN'], { type: 'region' }) : null;
const countryNameFormatterEn = typeof Intl.DisplayNames === 'function' ? new Intl.DisplayNames(['en'], { type: 'region' }) : null;
const countryNamesBySlug = buildCountryNamesBySlug();
const countryNamesByCallingCode = buildCountryNamesByCallingCode();
const SMS_COUNTRY_FALLBACKS: SmsReceiverCountry[] = [
  { provider: 'country', value: 'country:colombia', name: 'Colombia', providers: { smsbower: 33, '5sim': 'colombia' } },
  { provider: 'country', value: 'country:chile', name: 'Chile', providers: { smsbower: 151, '5sim': 'chile' } },
  { provider: 'country', value: 'country:england', name: 'England', providers: { smsbower: 16, '5sim': 'england' } },
  { provider: 'country', value: 'country:brazil', name: 'Brazil', providers: { smsbower: 73, '5sim': 'brazil' } },
  { provider: 'country', value: 'country:indonesia', name: 'Indonesia', providers: { smsbower: 6, '5sim': 'indonesia' } },
  { provider: 'country', value: 'country:southafrica', name: 'South Africa', providers: { smsbower: 31, '5sim': 'southafrica' } },
  { provider: 'country', value: 'country:philippines', name: 'Philippines', providers: { smsbower: 4, '5sim': 'philippines' } },
];
export function WaAccountAdd({ className, disabled, footer, onChanged, onDone, onError }: Props) {
  const isMobile = useIsMobile();
  const [phone, setPhone] = useState('');
  const [countryCallingCode, setCountryCallingCode] = useState('');
  const [probe, setProbe] = useState<WaAccountAddProbeState>(null);
  const [pending, setPending] = useState<PendingRegistration | null>(null);
  const [registrationResult, setRegistrationResult] = useState<WaWorkflowResponse | null>(null);
  const [registrationTarget, setRegistrationTarget] = useState<WaResolvedPhone | null>(null);
  const [smsReceiverOrder, setSmsReceiverOrder] = useState<SmsReceiverOrder | null>(null);
  const [smsProviders, setSmsProviders] = useState<SmsProviderName[]>(SMS_PROVIDER_OPTIONS.map((option) => option.value));
  const [smsCountries, setSmsCountries] = useState(() => storedTextValue(SMS_COUNTRIES_STORAGE_KEY, DEFAULT_SMS_COUNTRIES));
  const [smsMaxPrice, setSmsMaxPrice] = useState('0.35');
  const [smsWaitSeconds, setSmsWaitSeconds] = useState('180');
  const [smsTargetCount, setSmsTargetCount] = useState(() => storedTextValue(SMS_TARGET_COUNT_STORAGE_KEY, DEFAULT_SMS_TARGET_COUNT));
  const [smsProxyMode, setSmsProxyMode] = useState<SmsProxyMode>('direct');
  const [smsProviderPickerOpen, setSmsProviderPickerOpen] = useState(false);
  const [smsCountryPickerOpen, setSmsCountryPickerOpen] = useState(false);
  const [smsCountryOptions, setSmsCountryOptions] = useState<SmsReceiverCountry[]>(SMS_COUNTRY_FALLBACKS);
  const [smsCountryOptionsLoading, setSmsCountryOptionsLoading] = useState(false);
  const [smsCountryOptionsError, setSmsCountryOptionsError] = useState('');
  const [smsSettingsOpen, setSmsSettingsOpen] = useState(false);
  const [smsSettingsSpinKey, setSmsSettingsSpinKey] = useState(0);
  const [smsSettings, setSmsSettings] = useState<SmsReceiverSettings | null>(null);
  const [smsSettingsDraft, setSmsSettingsDraft] = useState<SmsSettingsDraft>(() => emptySmsSettingsDraft());
  const [smsSettingsLoading, setSmsSettingsLoading] = useState(false);
  const [smsSettingsSaveState, setSmsSettingsSaveState] = useState<SmsSettingsSaveState>('idle');
  const [smsSettingsError, setSmsSettingsError] = useState('');
  const [proxyTest, setProxyTest] = useState<ProxyTestState>({ loading: false });
  const [smsProviderBalances, setSmsProviderBalances] = useState<Record<SmsProviderName, SmsProviderBalanceState>>({ smsbower: {}, herosms: {}, '5sim': {} });
  const [autoRunning, setAutoRunning] = useState(false);
  const [autoAttempts, setAutoAttempts] = useState(0);
  const [autoMessage, setAutoMessage] = useState('');
  const [autoTargetCount, setAutoTargetCount] = useState(0);
  const [autoSuccessCount, setAutoSuccessCount] = useState(0);
  const [autoFailedCount, setAutoFailedCount] = useState(0);
  const [autoSkippedCount, setAutoSkippedCount] = useState(0);
  const [cooldownStartedAt, setCooldownStartedAt] = useState(Date.now());
  const [clockNow, setClockNow] = useState(Date.now());
  const [otp, setOtp] = useState('');
  const [busy, setBusy] = useState(false);
  const autoNotifiedTaskRef = useRef('');
  const autoRunningRef = useRef(false);
  const smsSettingsDraftRef = useRef(smsSettingsDraft);
  const smsSettingsSavedSignatureRef = useRef(smsSettingsDraftSignature(smsSettingsDraft));
  const smsSettingsSaveSeqRef = useRef(0);
  const samePhone = probeMatchesValues(probe, phone, countryCallingCode);
  const currentTarget = resolveWaPhoneTarget(phone, countryCallingCode).target;
  const hasPhoneTarget = Boolean(currentTarget);
  const registrationSamePhone = Boolean(registrationTarget && currentTarget?.e164 === registrationTarget.e164);
  const activeRegistrationResult = registrationSamePhone ? registrationResult : null;
  const status = waProbeStatus(activeRegistrationResult || (samePhone ? probe?.result : null));
  const channelStatus = samePhone ? waProbeStatus(activeRegistrationResult || probe?.result) : null;
  const cooldownElapsedSeconds = Math.max(0, (clockNow - cooldownStartedAt) / 1000);
  const blocked = status.blocked === true;
  const channelsHardBlocked = registrationChannelsHardBlocked(channelStatus);
  const nextCooldownSeconds = registrationMinimumCooldownSeconds(channelStatus, cooldownElapsedSeconds);
  const canRegister = samePhone && registrationAnyMethodAvailable(channelStatus, cooldownElapsedSeconds) && !channelsHardBlocked;
  const detected = samePhone && Boolean(channelStatus);
  const badgeVariant = pending ? 'default' : blocked ? 'destructive' : canRegister ? 'default' : detected ? 'secondary' : 'outline';
  const badgeLabel = accountAddBadgeLabel(Boolean(pending), blocked, canRegister, nextCooldownSeconds, detected);
  const showPhoneChanged = !autoRunning && Boolean(probe && !samePhone);
  const showStatusBar = autoRunning || Boolean(smsReceiverOrder) || showPhoneChanged;
  const smsProvider = smsProviders.length === 1 ? smsProviders[0] : '';
  const smsInputLocked = busy || disabled || Boolean(pending) || autoRunning;
  const channelGridClass = 'grid min-w-0 gap-2 sm:grid-cols-[108px_minmax(150px,1fr)_84px_88px] lg:grid-cols-[104px_minmax(130px,1fr)_82px_88px_68px_84px_112px]';
  const countryCallingName = countryNameFromCallingCode(countryCallingCode);

  useEffect(() => {
    smsSettingsDraftRef.current = smsSettingsDraft;
  }, [smsSettingsDraft]);

  const applyAutoTask = useCallback((task: AutoSmsRegistrationTask | null) => {
    setAutoRunning(Boolean(task?.running));
    setAutoAttempts(task?.attempt || 0);
    setAutoMessage(task?.message || '');
    const taskTargetCount = autoTaskTargetCount(task?.target_count);
    setAutoTargetCount(taskTargetCount);
    if (task?.running) {
      if (typeof task.target_count === 'number') setSmsTargetCount(String(taskTargetCount));
      const taskProviders = autoTaskProviders(task.providers);
      if (taskProviders.length) setSmsProviders(taskProviders);
      const taskCountries = autoTaskCountriesValue(task.countries);
      if (taskCountries) setSmsCountries(taskCountries);
      const taskMaxPrice = autoTaskTextValue(task.max_price);
      if (taskMaxPrice) setSmsMaxPrice(taskMaxPrice);
      if (typeof task.timeout_seconds === 'number' && Number.isFinite(task.timeout_seconds) && task.timeout_seconds > 0) setSmsWaitSeconds(String(task.timeout_seconds));
      const taskProxyMode = autoTaskProxyMode(task.proxy_mode);
      if (taskProxyMode) setSmsProxyMode(taskProxyMode);
    }
    setAutoSuccessCount(task?.success_count || 0);
    setAutoFailedCount(task?.failed_count || 0);
    setAutoSkippedCount(task?.skipped_count || 0);
    if (task?.running && !task.last_result) {
      setProbe(null);
      setRegistrationResult(null);
      setRegistrationTarget(null);
    }
    if (task?.running && task.sms_receiver_order) setSmsReceiverOrder(task.sms_receiver_order);
    if (task?.id && !task.running && task.status !== 'idle') setSmsReceiverOrder(null);
    if (task?.running && task.sms_receiver_order?.phone) {
      const resolved = resolveInternationalWaPhone(task.sms_receiver_order.phone);
      if (resolved.target) {
        setPhone(resolved.target.input.phone);
        setCountryCallingCode(`+${resolved.target.input.country_calling_code}`);
        if (task.last_result) {
          const now = Date.now();
          setCooldownStartedAt(now);
          setClockNow(now);
          setProbe({ target: resolved.target, result: task.last_result });
          setRegistrationResult(null);
          setRegistrationTarget(null);
        }
        return;
      }
    }
    const currentPhone = task?.current_phone || {};
    if (task?.running && task.last_result) {
      const resolved = resolveAutoTaskPhone(currentPhone);
      if (resolved?.target) {
        setPhone(resolved.target.input.phone);
        setCountryCallingCode(`+${resolved.target.input.country_calling_code}`);
        const now = Date.now();
        setCooldownStartedAt(now);
        setClockNow(now);
        setProbe({ target: resolved.target, result: task.last_result });
        setRegistrationResult(null);
        setRegistrationTarget(null);
        return;
      }
    }
    const callingCode = typeof currentPhone.country_calling_code === 'string' ? currentPhone.country_calling_code : '';
    if (task?.running && callingCode) setCountryCallingCode(`+${callingCode.replace(/^\+/, '')}`);
  }, []);

  const applySmsSettingsToForm = useCallback((settings: SmsReceiverSettings) => {
    const providers = normalizeSmsProviderSelection((settings.providerOrder || []) as SmsProviderName[]);
    if (providers.length) setSmsProviders(providers);
    if (typeof settings.maxPrice === 'number' && Number.isFinite(settings.maxPrice) && settings.maxPrice > 0) setSmsMaxPrice(String(settings.maxPrice));
    if (typeof settings.timeoutSeconds === 'number' && Number.isFinite(settings.timeoutSeconds) && settings.timeoutSeconds > 0) setSmsWaitSeconds(String(settings.timeoutSeconds));
  }, []);

  const loadSmsSettings = useCallback(async (options: { applyForm?: boolean } = {}) => {
    setSmsSettingsLoading(true);
    setSmsSettingsError('');
    try {
      const result = await getSmsReceiverSettings();
      if (result.success === false || result.error_message) throw new Error(result.error_message || '接码设置加载失败');
      const settings = result.settings || {};
      const draft = smsSettingsDraftFromSettings(settings);
      setSmsSettings(settings);
      setSmsSettingsDraft(draft);
      smsSettingsSavedSignatureRef.current = smsSettingsDraftSignature(draft);
      setSmsSettingsSaveState('saved');
      if (options.applyForm && !autoRunningRef.current) applySmsSettingsToForm(settings);
    } catch (error) {
      setSmsSettingsError(error instanceof Error ? error.message : String(error));
      setSmsSettingsSaveState('error');
    } finally {
      setSmsSettingsLoading(false);
    }
  }, [applySmsSettingsToForm]);

  useEffect(() => {
    autoRunningRef.current = autoRunning;
  }, [autoRunning]);

  useEffect(() => {
    const activeResult = activeRegistrationResult || (samePhone ? probe?.result : null);
    if (!activeResult) return undefined;
    const timer = window.setInterval(() => setClockNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [activeRegistrationResult, probe, samePhone]);

  useEffect(() => {
    if (busy || disabled || pending || autoRunning) {
      setSmsCountryPickerOpen(false);
      setSmsProviderPickerOpen(false);
    }
  }, [autoRunning, busy, disabled, pending]);

  useEffect(() => {
    if (!smsSettingsOpen) return;
    void loadSmsSettings();
  }, [loadSmsSettings, smsSettingsOpen]);

  useEffect(() => {
    void loadSmsSettings({ applyForm: true });
  }, [loadSmsSettings]);

  useEffect(() => {
    const normalized = formatSmsCountryValues(parseSmsCountryValues(smsCountries, smsProvider));
    if (normalized) storeTextValue(SMS_COUNTRIES_STORAGE_KEY, normalized);
  }, [smsCountries, smsProvider]);

  useEffect(() => {
    const targetCount = autoTargetCountInput(smsTargetCount);
    if (targetCount !== null) storeTextValue(SMS_TARGET_COUNT_STORAGE_KEY, String(targetCount));
  }, [smsTargetCount]);

  useEffect(() => {
    let alive = true;
    const syncAutoTask = async () => {
      try {
        const result = await getAutoSmsRegistration();
        if (!alive) return;
        applyAutoTask(result.task || null);
        const task = result.task;
        if (task?.id && !task.running && task.status === 'success' && autoNotifiedTaskRef.current !== task.id) {
          autoNotifiedTaskRef.current = task.id;
          onDone(`自动接码已完成 ${task.success_count || 0}/${task.target_count || 0}`);
          void onChanged();
        }
        if (task?.id && !task.running && task.status === 'error' && autoNotifiedTaskRef.current !== task.id) {
          autoNotifiedTaskRef.current = task.id;
          onError(task.error_message || task.message || '自动接码失败');
        }
      } catch {
        if (alive) applyAutoTask(null);
      }
    };
    void syncAutoTask();
    const timer = window.setInterval(() => void syncAutoTask(), autoRunning ? 1500 : 4000);
    return () => {
      alive = false;
      window.clearInterval(timer);
    };
  }, [applyAutoTask, autoRunning, onChanged, onDone, onError]);

  useEffect(() => {
    setSmsCountries((current) => {
      const values = parseSmsCountryValues(current, smsProvider);
      if (values.length) return formatSmsCountryValues(values);
      return DEFAULT_SMS_COUNTRIES;
    });
  }, [smsProvider]);

  useEffect(() => {
    const controller = new AbortController();
    const baseInput = { provider: smsProvider || undefined, providers: smsProvider ? undefined : smsProviders, service: 'wa' };
    const configuredProxy = smsSettingsDraft.proxy.trim();
    const useDirectFirst = smsProxyMode === 'direct' || !configuredProxy;
    let fallbackToDirect = false;
    setSmsCountryOptionsLoading(true);
    setSmsCountryOptionsError('');
    const loadCountries = async () => {
      try {
        const firstResult = await getSmsReceiverCountries({ ...baseInput, proxy: useDirectFirst ? '' : undefined }, { signal: controller.signal });
        if (useDirectFirst || !smsCountryShouldFallbackToDirect(firstResult)) return firstResult;
      } catch (error) {
        if (isAbortError(error) || useDirectFirst) throw error;
      }
      fallbackToDirect = true;
      return getSmsReceiverCountries({ ...baseInput, proxy: '' }, { signal: controller.signal });
    };
    loadCountries()
      .then((result) => {
        const countries = Array.isArray(result.countries) ? result.countries : [];
        const merged = countries.length ? mergeSmsCountryOptions(countries, SMS_COUNTRY_FALLBACKS) : SMS_COUNTRY_FALLBACKS;
        setSmsCountryOptions(merged);
        setSmsCountries((current) => formatSmsCountryValues(parseManualSmsCountryValues(current, smsProvider, merged)) || current);
        const providerErrors = smsCountryProviderErrorsMessage(result.errors);
        if (fallbackToDirect && countries.length) {
          setSmsCountryOptionsError('配置代理不可用，已改用直连加载国家列表');
        } else if (!countries.length) {
          setSmsCountryOptionsError('国家列表加载失败，请检查接码平台 Key 或网络');
        } else if (result.error_message || providerErrors) {
          setSmsCountryOptionsError(smsCountryLoadErrorMessage(result.error_message || providerErrors));
        }
      })
      .catch((error: unknown) => {
        if (isAbortError(error)) return;
        setSmsCountryOptions(SMS_COUNTRY_FALLBACKS);
        setSmsCountryOptionsError(smsCountryLoadErrorMessage(error instanceof Error ? error.message : String(error)));
      })
      .finally(() => {
        if (!controller.signal.aborted) setSmsCountryOptionsLoading(false);
      });
    return () => controller.abort();
  }, [smsProvider, smsProviders, smsProxyMode, smsSettingsDraft.proxy]);

  async function runProbe() {
    const resolved = resolveWaPhoneTarget(phone, countryCallingCode);
    if (!resolved.target) return onError(resolved.error || '请输入手机号和国家拨号码');
    setBusy(true);
    try {
      setRegistrationResult(null);
      setRegistrationTarget(null);
      setPending(null);
      const result = await probeWaPhoneSMS(resolved.target.input);
      resetCooldownClock();
      setProbe({ target: resolved.target, result });
    } catch (error) {
      onError(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  }
  async function discardSmsReceiverOrder(order: SmsReceiverOrder | null) {
    if (!order) return;
    try {
      await cancelSmsReceiverOrder(order);
    } catch {
      // Best-effort cleanup only. The visible workflow error should stay focused on registration.
    }
  }
  async function completeSmsReceiverOrder(order: SmsReceiverOrder | null) {
    if (!order) return;
    try {
      await finishSmsReceiverOrder(order);
    } catch {
      // Best-effort cleanup only. Registration already succeeded.
    }
  }
  function clearManualPhoneInput(next: () => void) {
    const order = smsReceiverOrder;
    next();
    setSmsReceiverOrder(null);
    if (order) void discardSmsReceiverOrder(order);
  }
  async function submitOTP() {
    if (!pending) return onError('没有等待中的 OTP');
    const currentPending = pending;
    const code = otp.trim();
    if (!code) return onError('请输入 OTP');
    if (code.length !== WA_REGISTRATION_OTP_LENGTH) return onError(`请输入 ${WA_REGISTRATION_OTP_LENGTH} 位 OTP`);
    setBusy(true);
    try {
      const result = await submitWaRegistrationOTP(currentPending.accountID, code);
      if (result.success === false || result.error_message) throw new Error(accountReasonLabel(result.error_message, result.status) || 'OTP 提交失败');
      await completeSmsReceiverOrder(currentPending.smsReceiverOrder || null);
      setOtp('');
      setPending(null);
      setSmsReceiverOrder(null);
      onDone('OTP 已提交');
      await onChanged();
    } catch (error) {
      await discardSmsReceiverOrder(currentPending.smsReceiverOrder || null);
      setPending(null);
      setSmsReceiverOrder(null);
      onError(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  }
  async function receiveSMSOTP() {
    if (!pending) return onError('没有等待中的 OTP');
    const currentPending = pending;
    setBusy(true);
    try {
      const result = await receiveWaRegistrationSMSOTP({ waAccountID: currentPending.accountID, verificationRequestID: currentPending.verificationRequestID, timeoutSeconds: smsReceiverWaitSeconds(smsWaitSeconds) });
      if (result.success === false || result.error_message) throw new Error(accountReasonLabel(result.error_message, result.status) || '自动取码提交失败');
      setOtp('');
      setPending(null);
      setSmsReceiverOrder(null);
      onDone('OTP 已自动提交');
      await onChanged();
    } catch (error) {
      await discardSmsReceiverOrder(currentPending.smsReceiverOrder || null);
      setPending(null);
      setSmsReceiverOrder(null);
      onError(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  }
  async function stopAutoRegistration() {
    setAutoMessage('正在停止');
    try {
      const result = await stopAutoSmsRegistration();
      applyAutoTask(result.task || null);
      onDone('自动接码已停止');
    } catch (error) {
      onError(error instanceof Error ? error.message : String(error));
    }
  }
  async function allocateSingleSMSNumber() {
    if (autoRunning) return;
    const allocation = smsReceiverAllocationInput({ smsProviders, smsProvider, smsCountries, smsMaxPrice, smsWaitSeconds, smsProxyMode });
    if (!allocation.ok) return onError(allocation.error);
    const previousOrder = smsReceiverOrder;
    setBusy(true);
    try {
      setRegistrationResult(null);
      setRegistrationTarget(null);
      setPending(null);
      setProbe(null);
      setOtp('');
      const result = await allocateSmsReceiverOrder(allocation.input);
      if (result.success === false || result.error_message) throw new Error(result.error_message || '单次拿号失败');
      const order = result.order || result.sms_receiver_order || null;
      if (!order?.phone) throw new Error('单次拿号失败：接码平台未返回手机号');
      if (previousOrder && (previousOrder.provider !== order.provider || previousOrder.id !== order.id)) {
        await discardSmsReceiverOrder(previousOrder);
      }
      setSmsReceiverOrder(order);
      applySmsReceiverPhone(order.phone);
      onDone('单次号码已获取');
    } catch (error) {
      onError(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  }
  async function startAutoSMSNumber() {
    if (autoRunning) return;
    const allocation = smsReceiverAllocationInput({ smsProviders, smsProvider, smsCountries, smsMaxPrice, smsWaitSeconds, smsProxyMode });
    if (!allocation.ok) return onError(allocation.error);
    const targetCount = autoTargetCountInput(smsTargetCount);
    if (targetCount === null) return onError('请输入有效目标数，0 表示不限');
    setBusy(true);
    setAutoRunning(true);
    setAutoAttempts(0);
    setAutoTargetCount(targetCount);
    setAutoSuccessCount(0);
    setAutoFailedCount(0);
    setAutoSkippedCount(0);
    setAutoMessage('准备自动接码');
    try {
      setRegistrationResult(null);
      setRegistrationTarget(null);
      setPending(null);
      setOtp('');
      const result = await startAutoSmsRegistration(allocation.input, targetCount);
      if (result.success === false || result.error_message) throw new Error(result.error_message || '自动接码启动失败');
      applyAutoTask(result.task || null);
    } catch (error) {
      setAutoRunning(false);
      onError(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  }
  async function startRegistration(method: SelectableRegistrationMethodOption) {
    const resolved = resolveWaPhoneTarget(phone, countryCallingCode);
    if (!resolved.target) return onError(resolved.error || '请输入手机号和国家拨号码');
    if (!samePhone || !channelStatus) return onError('请先检测验证通道');
    setBusy(true);
    try {
      const result = await registerWaPhone(resolved.target.input, method.value, smsReceiverOrder || undefined);
      const resultStatus = waProbeStatus(result);
      resetCooldownClock();
      setRegistrationResult(result);
      setRegistrationTarget(resolved.target);
      if (result.success === false || result.error_message || resultStatus.blocked === true || resultStatus.requestFailed) {
        await discardSmsReceiverOrder(smsReceiverOrder);
        setSmsReceiverOrder(null);
        onError(registrationFailureMessage(result, resultStatus));
        return;
      }
      const accountID = workflowText(result, 'wa_account_id');
      const verificationRequestID = workflowText(result, 'verification_request_id');
      if (accountID) setPending({ accountID, verificationRequestID, smsReceiverOrder: result.sms_receiver_order || smsReceiverOrder || undefined });
      setProbe(null);
      setOtp('');
      onDone(accountID ? 'OTP 已发送' : '已发起');
      await onChanged();
    } catch (error) {
      onError(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  }
  function resetCooldownClock() {
    const now = Date.now();
    setCooldownStartedAt(now);
    setClockNow(now);
  }
  function applySmsReceiverPhone(value: string) {
    const resolved = resolveInternationalWaPhone(value);
    if (resolved.target) {
      setPhone(resolved.target.input.phone);
      setCountryCallingCode(`+${resolved.target.input.country_calling_code}`);
      return;
    }
    setPhone(value);
  }
  const saveSmsSettingsDraft = useCallback(async (draft: SmsSettingsDraft) => {
    const providers = normalizeSmsProviderSelection(draft.providers);
    if (!providers.length) {
      setSmsSettingsError('至少启用一个接码平台');
      setSmsSettingsSaveState('error');
      return;
    }
    const signature = smsSettingsDraftSignature(draft);
    const providerPayload = smsSettingsProviderPayload(draft);
    const seq = smsSettingsSaveSeqRef.current + 1;
    smsSettingsSaveSeqRef.current = seq;
    setSmsSettingsSaveState('saving');
    setSmsSettingsError('');
    try {
      const result = await saveSmsReceiverSettings({
        proxy: draft.proxy.trim(),
        providerOrder: providers,
        service: 'wa',
        providers: providerPayload,
      });
      if (result.success === false || result.error_message) throw new Error(result.error_message || '接码设置保存失败');
      if (smsSettingsSaveSeqRef.current !== seq) return;
      const settings = result.settings || {};
      setSmsSettings(settings);
      setSmsProviders(providers);
      smsSettingsSavedSignatureRef.current = signature;
      if (smsSettingsSaveSeqRef.current === seq && smsSettingsDraftSignature(smsSettingsDraftRef.current) === signature) {
        const nextDraft = smsSettingsDraftFromSettings(settings);
        setSmsSettingsDraft(nextDraft);
        smsSettingsSavedSignatureRef.current = smsSettingsDraftSignature(nextDraft);
      }
      if (!draft.proxy.trim()) setSmsProxyMode('direct');
      setSmsSettingsSaveState('saved');
    } catch (error) {
      if (smsSettingsSaveSeqRef.current !== seq) return;
      setSmsSettingsError(error instanceof Error ? error.message : String(error));
      setSmsSettingsSaveState('error');
    }
  }, []);
  async function runProxyTest(mode: 'direct' | 'proxy') {
    setProxyTest((current) => ({ ...current, loading: true, error: '' }));
    try {
      const result = await testSmsReceiverProxy({ mode, proxy: smsSettingsDraft.proxy.trim() });
      if (result.success === false || result.error_message) throw new Error(result.error_message || '代理测试失败');
      setProxyTest((current) => ({ ...current, loading: false, [mode]: result.result || { ok: false, mode, error: '测试没有返回结果' } }));
    } catch (error) {
      setProxyTest((current) => ({ ...current, loading: false, error: error instanceof Error ? error.message : String(error) }));
    }
  }
  async function runProviderBalance(provider: SmsProviderName) {
    setSmsProviderBalances((current) => ({ ...current, [provider]: { loading: true } }));
    try {
      const proxy = smsProxyMode === 'direct' ? '' : smsSettingsDraft.proxy.trim();
      const result = await querySmsReceiverBalance({ provider, proxy });
      if (result.success === false || result.error_message) throw new Error(result.error_message || '余额查询失败');
      const balance = result.balances?.[provider] || { ok: false, provider, error: '余额查询没有返回结果' };
      setSmsProviderBalances((current) => ({ ...current, [provider]: { loading: false, result: balance, error: balance.ok === false ? balance.error || '余额查询失败' : '' } }));
    } catch (error) {
      setSmsProviderBalances((current) => ({ ...current, [provider]: { loading: false, error: error instanceof Error ? error.message : String(error) } }));
    }
  }
  function toggleSmsSettings() {
    setSmsSettingsSpinKey((value) => value + 1);
    setSmsSettingsOpen((open) => !open);
  }

  useEffect(() => {
    if (!smsSettingsOpen || smsSettingsLoading) return undefined;
    const signature = smsSettingsDraftSignature(smsSettingsDraft);
    if (signature === smsSettingsSavedSignatureRef.current) return undefined;
    const timer = window.setTimeout(() => {
      void saveSmsSettingsDraft(smsSettingsDraft);
    }, 800);
    return () => window.clearTimeout(timer);
  }, [saveSmsSettingsDraft, smsSettingsDraft, smsSettingsLoading, smsSettingsOpen]);
  return (
    <div
      className={cn(
        'mx-auto grid w-full min-w-0 max-w-[820px] grid-cols-1 gap-4 transition-[max-width,grid-template-columns,gap] duration-300 ease-out xl:grid-cols-[minmax(0,820px)_0px] xl:items-start xl:gap-0',
        smsSettingsOpen && 'xl:max-w-[1216px] xl:grid-cols-[minmax(0,820px)_380px] xl:gap-4',
        className,
      )}
    >
      <Card className="min-w-0 gap-0 overflow-visible rounded-xl py-0">
        <CardHeader className="flex min-w-0 flex-row items-start justify-between gap-3 px-4 pt-4 pb-2">
          <div className="grid min-w-0 gap-1"><CardTitle className="inline-flex min-w-0 items-center gap-2 text-base"><WhatsAppIcon className="size-5 shrink-0" />添加 WAAccount</CardTitle></div>
          <div className="flex shrink-0 items-center gap-2">
            <Badge variant={badgeVariant}>
              {canRegister ? <CheckCircle2 size={12} /> : null}
              {badgeLabel}
            </Badge>
            <Button type="button" size="icon-sm" variant="ghost" className={cn(smsSettingsOpen && 'bg-muted text-foreground')} title="接码设置" aria-label="接码设置" onClick={toggleSmsSettings}>
              <Settings key={smsSettingsSpinKey} size={14} className="[animation:spin_320ms_ease-in-out_1]" />
            </Button>
          </div>
        </CardHeader>
        <CardContent className="grid min-w-0 gap-3 px-4 pb-4">
          {showStatusBar ? (
            <ChannelStatusBar
              autoAttempts={autoAttempts}
              autoFailedCount={autoFailedCount}
              autoMessage={autoMessage}
              autoRunning={autoRunning}
              autoSkippedCount={autoSkippedCount}
              autoSuccessCount={autoSuccessCount}
              autoTargetCount={autoTargetCount}
              disabled={disabled}
              phoneChanged={showPhoneChanged}
              smsReceiverOrder={smsReceiverOrder}
              onStop={stopAutoRegistration}
            />
          ) : null}
          <Field>
            <FieldLabel>通道</FieldLabel>
            <div className="grid min-w-0 gap-2 rounded-md border bg-muted/20 p-2">
              <div className={channelGridClass}>
                <Field>
                  <FieldLabel>接码平台</FieldLabel>
                  <SmsProviderPicker
                    disabled={smsInputLocked}
                    onChange={setSmsProviders}
                    onOpenChange={setSmsProviderPickerOpen}
                    open={smsProviderPickerOpen}
                    value={smsProviders}
                  />
                </Field>
                <Field>
                  <FieldLabel>{smsProvider === '5sim' ? '国家' : smsProvider ? '国家 ID' : '国家/ID'}</FieldLabel>
                  <SmsCountryPicker
                    countries={smsCountryOptions}
                    provider={smsProvider}
                    loading={smsCountryOptionsLoading}
                    loadError={smsCountryOptionsError}
                    value={smsCountries}
                    onChange={setSmsCountries}
                    disabled={smsInputLocked}
                    open={smsCountryPickerOpen}
                    onOpenChange={setSmsCountryPickerOpen}
                  />
                </Field>
                <Field><FieldLabel>最高价</FieldLabel><Input type="number" min="0" step="0.01" value={smsMaxPrice} onChange={(event) => setSmsMaxPrice(event.target.value)} disabled={smsInputLocked} /></Field>
                <Field><FieldLabel>等码秒数</FieldLabel><Input type="number" min="30" step="10" value={smsWaitSeconds} onChange={(event) => setSmsWaitSeconds(event.target.value)} disabled={smsInputLocked} /></Field>
                <Field><FieldLabel>目标数</FieldLabel><Input type="number" min="0" step="1" value={smsTargetCount} onChange={(event) => setSmsTargetCount(event.target.value)} disabled={smsInputLocked} /></Field>
                <Field>
                  <FieldLabel>代理</FieldLabel>
                  <Select value={smsProxyMode} onValueChange={(value) => setSmsProxyMode(value as SmsProxyMode)} disabled={smsInputLocked}>
                    <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="direct">直连</SelectItem>
                      <SelectItem value="config">配置</SelectItem>
                    </SelectContent>
                  </Select>
                </Field>
                <Field>
                  <FieldLabel>拿号</FieldLabel>
                  <div className="grid grid-cols-2 gap-1">
                    <Button type="button" variant="secondary" className="px-2" disabled={smsInputLocked} onClick={() => void allocateSingleSMSNumber()}>
                      单次
                    </Button>
                    {autoRunning ? (
                      <Button type="button" variant="secondary" className="px-2" disabled>
                        运行中
                      </Button>
                    ) : (
                      <Button type="button" variant="secondary" className="px-2" disabled={smsInputLocked} onClick={() => void startAutoSMSNumber()}>
                        自动
                      </Button>
                    )}
                  </div>
                </Field>
              </div>
            </div>
          </Field>
          <FieldGroup>
            <div className="grid min-w-0 gap-3 sm:grid-cols-[160px_1fr]">
              <Field>
                <FieldLabel>国家拨号码</FieldLabel>
                <CountryCallingCodeInput
                  countryName={countryCallingName}
                  disabled={busy || disabled || autoRunning}
                  value={countryCallingCode}
                  onChange={(nextValue) => clearManualPhoneInput(() => setCountryCallingCode(nextValue))}
                />
              </Field>
              <Field>
                <FieldLabel>手机号</FieldLabel>
              <div className="flex min-w-0 gap-2">
                  <Input placeholder="4155550123" value={phone} onChange={(event) => clearManualPhoneInput(() => setPhone(event.target.value))} disabled={busy || disabled || autoRunning} />
                  <Button type="button" size="icon" variant="outline" disabled={busy || disabled || autoRunning} title="检测手机号" aria-label="检测手机号" onClick={() => void runProbe()}><Search size={14} /></Button>
                </div>
              </Field>
            </div>
          </FieldGroup>
          <Field>
            <WaRegistrationChannelButtons
              status={channelStatus}
              elapsedSeconds={cooldownElapsedSeconds}
              phoneReady={hasPhoneTarget}
              disabled={busy || disabled || Boolean(pending) || autoRunning || channelsHardBlocked}
              onStart={(method) => void startRegistration(method)}
            />
            <p className="px-1 text-[11px] text-muted-foreground">「旧设备」用于从真机转入账号：真机 WhatsApp 仍在线时，验证码会发送到真机，读取后填入下方完成转入。</p>
          </Field>
          {pending && <WaRegistrationOtpCard value={otp} busy={busy} autoReceiveEnabled={Boolean(pending.smsReceiverOrder)} onChange={setOtp} onSubmit={() => void submitOTP()} onAutoReceive={() => void receiveSMSOTP()} />}
        </CardContent>
        {footer ? <div className="overflow-hidden rounded-b-xl border-t">{footer}</div> : null}
      </Card>
      <div className={cn('hidden min-w-0 overflow-hidden transition-opacity duration-300 ease-out xl:block', smsSettingsOpen ? 'opacity-100' : 'pointer-events-none opacity-0')} aria-hidden={!smsSettingsOpen}>
        <SmsReceiverSettingsPanel
          balances={smsProviderBalances}
          draft={smsSettingsDraft}
          error={smsSettingsError}
          loading={smsSettingsLoading}
          open={smsSettingsOpen}
          proxyTest={proxyTest}
          proxyMode={smsProxyMode}
          saveState={smsSettingsSaveState}
          settings={smsSettings}
          onClose={() => setSmsSettingsOpen(false)}
          onDraftChange={setSmsSettingsDraft}
          onQueryBalance={(provider) => void runProviderBalance(provider)}
          onTestProxy={(mode) => void runProxyTest(mode)}
        />
      </div>
      <Sheet open={smsSettingsOpen && isMobile} onOpenChange={setSmsSettingsOpen}>
        <SheetContent side="right" className="w-[min(100vw,420px)] max-w-none p-0 [&>button]:hidden">
          <SheetHeader className="sr-only">
            <SheetTitle>接码设置</SheetTitle>
            <SheetDescription>配置接码代理和平台 Key。</SheetDescription>
          </SheetHeader>
          <SmsReceiverSettingsPanel
            balances={smsProviderBalances}
            className="h-full max-h-none w-full min-w-0 max-w-none rounded-none border-0 shadow-none"
            draft={smsSettingsDraft}
            error={smsSettingsError}
            loading={smsSettingsLoading}
            open={smsSettingsOpen}
            proxyTest={proxyTest}
            proxyMode={smsProxyMode}
            saveState={smsSettingsSaveState}
            settings={smsSettings}
            onClose={() => setSmsSettingsOpen(false)}
            onDraftChange={setSmsSettingsDraft}
            onQueryBalance={(provider) => void runProviderBalance(provider)}
            onTestProxy={(mode) => void runProxyTest(mode)}
          />
        </SheetContent>
      </Sheet>
    </div>
  );
}

function smsReceiverAllocationInput(input: { smsProviders: SmsProviderName[]; smsProvider: SmsProviderOption; smsCountries: string; smsMaxPrice: string; smsWaitSeconds: string; smsProxyMode: SmsProxyMode }): { ok: true; input: SmsReceiverAllocateInput } | { ok: false; error: string } {
  const providers = normalizeSmsProviderSelection(input.smsProviders);
  if (!providers.length) return { ok: false, error: '请选择接码平台' };
  const countries = parseSmsCountryValues(input.smsCountries, input.smsProvider);
  if (!countries.length) return { ok: false, error: input.smsProvider === '5sim' ? '请输入接码国家' : '请输入接码国家 ID' };
  const maxPrice = Number.parseFloat(input.smsMaxPrice);
  if (!Number.isFinite(maxPrice) || maxPrice <= 0) return { ok: false, error: '请输入有效最高价' };
  const timeoutSeconds = smsReceiverWaitSeconds(input.smsWaitSeconds);
  if (timeoutSeconds <= 0) return { ok: false, error: '请输入有效等码秒数' };
  return {
    ok: true,
    input: {
      provider: input.smsProvider || undefined,
      providers: input.smsProvider ? undefined : providers,
      service: 'wa',
      countries,
      maxPrice,
      timeoutSeconds,
      proxy: input.smsProxyMode === 'direct' ? '' : undefined,
    },
  };
}

function storedTextValue(key: string, fallback: string) {
  if (typeof window === 'undefined') return fallback;
  try {
    const stored = window.localStorage.getItem(key)?.trim();
    return stored || fallback;
  } catch {
    return fallback;
  }
}

function storeTextValue(key: string, value: string) {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // Local storage can be unavailable in private or restricted browser contexts.
  }
}

function CountryCallingCodeInput({ countryName, disabled, onChange, value }: { countryName: string; disabled?: boolean; onChange: (value: string) => void; value: string }) {
  return (
    <div
      className={cn(
        'flex h-8 w-full min-w-0 overflow-hidden rounded-lg border border-input bg-transparent text-base transition-colors focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50 md:text-sm dark:bg-input/30',
        disabled && 'pointer-events-none cursor-not-allowed bg-input/50 opacity-50 dark:bg-input/80'
      )}
    >
      {countryName ? (
        <span className="flex max-w-[6.5rem] shrink-0 items-center truncate border-r bg-muted/40 px-2 text-xs text-muted-foreground" title={countryName}>
          {countryName}
        </span>
      ) : null}
      <input
        className="h-full min-w-0 flex-1 bg-transparent px-2.5 py-1 outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed"
        placeholder="+1"
        value={value}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  );
}

function resolveAutoTaskPhone(phone: Record<string, unknown>) {
  const e164 = stringRecordValue(phone, 'e164_number');
  if (e164) {
    const resolved = resolveInternationalWaPhone(e164);
    if (resolved.target) return resolved;
  }
  const callingCode = stringRecordValue(phone, 'country_calling_code');
  const nationalNumber = stringRecordValue(phone, 'national_number');
  if (!callingCode || !nationalNumber) return null;
  return resolveInternationalWaPhone(`+${callingCode.replace(/^\+/, '')}${nationalNumber}`);
}

function autoTargetCountInput(value: string) {
  const trimmed = value.trim();
  if (!trimmed) return null;
  const parsed = Number.parseInt(trimmed, 10);
  if (!Number.isFinite(parsed) || parsed < 0) return null;
  return parsed;
}

function autoTaskTargetCount(value: unknown) {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : 0;
}

function autoTaskProviders(value: unknown) {
  if (!Array.isArray(value)) return [];
  const selected = new Set(value.map((item) => String(item || '').trim().toLowerCase()));
  return SMS_PROVIDER_OPTIONS.map((option) => option.value).filter((provider) => selected.has(provider));
}

function autoTaskTextValue(value: unknown) {
  if (typeof value !== 'string') return '';
  return value.trim();
}

function autoTaskProxyMode(value: unknown): SmsProxyMode | '' {
  const normalized = typeof value === 'string' ? value.trim().toLowerCase() : '';
  if (normalized === 'direct') return 'direct';
  if (normalized === 'config' || normalized === 'proxy') return 'config';
  return '';
}

function autoTaskCountriesValue(value: unknown) {
  if (typeof value !== 'string') return '';
  const trimmed = value.trim();
  if (!trimmed) return '';
  const jsonValues = parseJsonStringArray(trimmed);
  const rawParts = jsonValues || trimmed.replace(/^\[/, '').replace(/\]$/, '').split(/[,，、\s]+/);
  const values = rawParts.map((part) => smsCountryValueFromTaskText(part)).filter((part): part is number | string => part !== null);
  return formatSmsCountryValues(values);
}

function parseJsonStringArray(value: string) {
  if (!value.startsWith('[') || !value.endsWith(']')) return null;
  try {
    const parsed: unknown = JSON.parse(value);
    return Array.isArray(parsed) && parsed.every((item) => typeof item === 'string') ? parsed : null;
  } catch {
    // Task countries may also be formatted like [country:a country:b].
    return null;
  }
}

function smsCountryValueFromTaskText(value: string): number | string | null {
  const trimmed = value.trim().replace(/^["']|["']$/g, '');
  if (!trimmed) return null;
  if (/^\d+$/.test(trimmed)) return Number.parseInt(trimmed, 10);
  const slug = normalizeCountrySlug(trimmed);
  if (!slug) return null;
  return `country:${slug}`;
}

function autoProgressLabel(successCount: number, targetCount: number) {
  if (targetCount <= 0) return `成功 ${successCount}/不限`;
  return `成功 ${successCount}/${targetCount}`;
}

function stringRecordValue(record: Record<string, unknown>, key: string) {
  const value = record[key];
  return typeof value === 'string' ? value.trim() : '';
}

type ChannelStatusBarProps = {
  autoAttempts: number;
  autoFailedCount: number;
  autoMessage: string;
  autoRunning: boolean;
  autoSkippedCount: number;
  autoSuccessCount: number;
  autoTargetCount: number;
  disabled?: boolean;
  phoneChanged: boolean;
  smsReceiverOrder: SmsReceiverOrder | null;
  onStop: () => void;
};

function ChannelStatusBar({ autoAttempts, autoFailedCount, autoMessage, autoRunning, autoSkippedCount, autoSuccessCount, autoTargetCount, disabled, phoneChanged, smsReceiverOrder, onStop }: ChannelStatusBarProps) {
  return (
    <div className="flex h-10 items-center justify-between gap-2 overflow-hidden rounded-md border bg-muted/30 px-3 text-sm">
      <div className="flex min-w-0 items-center gap-2 overflow-hidden whitespace-nowrap">
        {autoRunning ? (
          <Badge variant="default" className="shrink-0">
            自动接码 · {autoProgressLabel(autoSuccessCount, autoTargetCount)} · 第 {autoAttempts} 次
          </Badge>
        ) : null}
        {autoRunning && autoMessage ? (
          <Badge variant="secondary" className="min-w-0 max-w-[260px] truncate" title={autoMessage}>
            {autoMessage}
          </Badge>
        ) : null}
        {autoRunning && (autoFailedCount > 0 || autoSkippedCount > 0) ? (
          <Badge variant="outline" className="shrink-0">
            失败 {autoFailedCount} · 跳过 {autoSkippedCount}
          </Badge>
        ) : null}
        {smsReceiverOrder ? (
          <Badge variant="secondary" className="shrink-0">
            接码平台 {smsReceiverOrder.provider}
          </Badge>
        ) : null}
        {phoneChanged ? (
          <Badge variant="outline" className="shrink-0 border-amber-300 bg-amber-50 text-amber-800">
            号码已变化，请重新检测
          </Badge>
        ) : null}
      </div>
      {autoRunning ? (
        <Button type="button" size="sm" variant="destructive" className="shrink-0" disabled={disabled} onClick={onStop}>
          <CircleStop size={14} />
          停止
        </Button>
      ) : null}
    </div>
  );
}

type SmsReceiverSettingsPanelProps = {
  balances: Record<SmsProviderName, SmsProviderBalanceState>;
  className?: string;
  draft: SmsSettingsDraft;
  error: string;
  loading: boolean;
  onDraftChange: (draft: SmsSettingsDraft) => void;
  onClose: () => void;
  onQueryBalance: (provider: SmsProviderName) => void;
  onTestProxy: (mode: 'direct' | 'proxy') => void;
  open: boolean;
  proxyTest: ProxyTestState;
  proxyMode: SmsProxyMode;
  saveState: SmsSettingsSaveState;
  settings: SmsReceiverSettings | null;
};

function SmsReceiverSettingsPanel({ balances, className, draft, error, loading, onClose, onDraftChange, onQueryBalance, onTestProxy, open, proxyMode, proxyTest, saveState, settings }: SmsReceiverSettingsPanelProps) {
  const providerSet = new Set(draft.providers);

  useEffect(() => {
    if (!open) return undefined;
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') onClose();
    }
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [onClose, open]);

  function updateDraft(patch: Partial<SmsSettingsDraft>) {
    onDraftChange({ ...draft, ...patch });
  }

  function toggleProvider(provider: SmsProviderName) {
    const next = new Set(draft.providers);
    if (next.has(provider)) {
      if (next.size <= 1) return;
      next.delete(provider);
    } else {
      next.add(provider);
    }
    updateDraft({ providers: SMS_PROVIDER_OPTIONS.map((option) => option.value).filter((providerName) => next.has(providerName)) });
  }

  function updateKey(provider: SmsProviderName, value: string) {
    updateDraft({ keys: { ...draft.keys, [provider]: value }, clearKeys: { ...draft.clearKeys, [provider]: false } });
  }

  function clearKey(provider: SmsProviderName) {
    if (draft.clearKeys[provider]) {
      updateDraft({ clearKeys: { ...draft.clearKeys, [provider]: false } });
      return;
    }
    if (draft.keys[provider]) {
      updateDraft({ keys: { ...draft.keys, [provider]: '' } });
      return;
    }
    const label = SMS_PROVIDER_OPTIONS.find((option) => option.value === provider)?.label || provider;
    if (!settings?.providers?.[provider]?.apiKeyConfigured || !window.confirm(`确定清除 ${label} Key？`)) return;
    updateDraft({ keys: { ...draft.keys, [provider]: '' }, clearKeys: { ...draft.clearKeys, [provider]: true } });
  }

  return (
    <aside
      className={cn(
        'grid h-[676px] max-h-[calc(100dvh-5rem)] w-[380px] min-w-[380px] max-w-[380px] shrink-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-xl border bg-card text-card-foreground shadow-sm transition-opacity duration-300 ease-out',
        open ? 'opacity-100' : 'pointer-events-none opacity-0',
        className,
      )}
    >
      <div className="flex items-start justify-between gap-3 border-b px-4 py-3">
          <div className="grid gap-1">
            <div className="flex items-center gap-2 font-medium">
              <span>接码设置</span>
              <SmsSettingsSaveBadge state={saveState} />
            </div>
            <div className="text-xs text-muted-foreground">配置接码代理和平台 Key。Key 留空表示不修改。</div>
          </div>
          <Button type="button" size="icon-sm" variant="ghost" title="关闭设置" aria-label="关闭设置" onClick={onClose}>
            <X size={14} />
          </Button>
        </div>
        <div className="thin-scrollbar grid gap-5 overflow-y-auto px-4 py-4">
          {error ? <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">{error}</div> : null}
          {proxyTest.error ? <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">{proxyTest.error}</div> : null}
          <div className="grid gap-3">
            <div className="font-medium">代理</div>
            <Field>
              <FieldLabel>服务器配置代理</FieldLabel>
              <Input
                placeholder="留空为直连，例如 http://user:pass@1.2.3.4:7880"
                value={draft.proxy}
                disabled={loading}
                onChange={(event) => updateDraft({ proxy: event.target.value })}
              />
            </Field>
            <div className="rounded-md border bg-muted/20 px-3 py-2 text-xs leading-5 text-muted-foreground">
              直连不走代理；配置代理从服务器发起。部署后 127.0.0.1 是服务器/容器。格式：<span className="font-mono">http://IP:端口</span> 或 <span className="font-mono">http://用户:密码@IP:端口</span>。
            </div>
            <div className="text-xs text-muted-foreground">当前拿号模式：{proxyMode === 'direct' ? '直连' : '配置代理'}</div>
            <div className="grid grid-cols-2 gap-2">
              <Button type="button" variant="outline" disabled={loading || proxyTest.loading} onClick={() => onTestProxy('direct')}>
                测试直连
              </Button>
              <Button type="button" variant="outline" disabled={loading || proxyTest.loading || !draft.proxy.trim()} onClick={() => onTestProxy('proxy')}>
                测试代理
              </Button>
            </div>
            <ProxyTestResultLine label="直连" result={proxyTest.direct} />
            <ProxyTestResultLine label="代理" result={proxyTest.proxy} />
          </div>
          <Separator />
          <div className="grid gap-3">
            <div className="font-medium">平台</div>
            {SMS_PROVIDER_OPTIONS.map((option) => {
              const provider = option.value;
              const providerSettings = settings?.providers?.[provider] || {};
              const balance = balances[provider] || {};
              const canClearKey = Boolean(providerSettings.apiKeyConfigured || draft.keys[provider] || draft.clearKeys[provider]);
              const canQueryBalance = Boolean(providerSettings.apiKeyConfigured && !draft.clearKeys[provider]);
              return (
                <div key={provider} className="grid gap-2 rounded-md border bg-muted/20 p-3">
                  <div className="flex items-center justify-between gap-3">
                    <button type="button" className="flex min-w-0 items-center gap-2 text-left font-medium" disabled={loading} onClick={() => toggleProvider(provider)}>
                      <span className="flex size-4 items-center justify-center rounded border bg-background">
                        {providerSet.has(provider) ? <Check size={12} /> : null}
                      </span>
                      <span>{option.label}</span>
                    </button>
                    <div className="flex shrink-0 items-center gap-1">
                      <span className="mr-1 text-xs text-muted-foreground">{providerSettings.apiKeyConfigured ? `已配置 ${providerSettings.apiKeyMasked || ''}` : '未配置'}</span>
                      <Button type="button" size="icon-sm" variant="ghost" title={`查询 ${option.label} 余额`} aria-label={`查询 ${option.label} 余额`} disabled={loading || balance.loading || !canQueryBalance} onClick={() => onQueryBalance(provider)}>
                        {balance.loading ? <Loader2 size={14} className="animate-spin" /> : <CircleDollarSign size={14} />}
                      </Button>
                      <Button
                        type="button"
                        size="icon-sm"
                        variant="ghost"
                        className={cn(draft.clearKeys[provider] && 'text-destructive hover:text-destructive')}
                        title={draft.clearKeys[provider] ? `取消清除 ${option.label} Key` : `清除 ${option.label} Key`}
                        aria-label={draft.clearKeys[provider] ? `取消清除 ${option.label} Key` : `清除 ${option.label} Key`}
                        disabled={loading || !canClearKey}
                        onClick={() => clearKey(provider)}
                      >
                        <Eraser size={14} />
                      </Button>
                    </div>
                  </div>
                  <Input
                    type="password"
                    placeholder="输入新 Key 才会替换"
                    value={draft.keys[provider]}
                    disabled={loading || draft.clearKeys[provider]}
                    onChange={(event) => updateKey(provider, event.target.value)}
                  />
                  <ProviderBalanceLine clearPending={draft.clearKeys[provider]} state={balance} />
                </div>
              );
            })}
          </div>
        </div>
    </aside>
  );
}

function SmsSettingsSaveBadge({ state }: { state: SmsSettingsSaveState }) {
  const baseClass = 'inline-flex w-14 shrink-0 justify-center rounded-full px-2 py-0.5 text-[11px] font-normal';
  if (state === 'saving') return <span className={cn(baseClass, 'bg-muted text-muted-foreground')}>保存中</span>;
  if (state === 'saved') return <span className={cn(baseClass, 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-200')}>已保存</span>;
  if (state === 'error') return <span className={cn(baseClass, 'bg-destructive/10 text-destructive')}>保存失败</span>;
  return <span className={baseClass} aria-hidden="true" />;
}

function ProviderBalanceLine({ clearPending, state }: { clearPending: boolean; state: SmsProviderBalanceState }) {
  if (clearPending) return <div className="text-xs text-destructive">正在清除这个 Key</div>;
  if (state.loading) return <div className="text-xs text-muted-foreground">余额查询中</div>;
  if (state.error) return <div className="break-words text-xs text-destructive">余额查询失败：{state.error}</div>;
  if (state.result?.ok === false) return <div className="break-words text-xs text-destructive">余额查询失败：{state.result.error || '未知错误'}</div>;
  if (state.result?.balance) {
    return <div className="text-xs text-muted-foreground">余额：{state.result.balance}{state.result.currency ? ` ${state.result.currency}` : ''}</div>;
  }
  return null;
}

function ProxyTestResultLine({ label, result }: { label: string; result?: SmsReceiverProxyTestResult }) {
  if (!result) return null;
  return (
    <div className={cn('rounded-md border px-3 py-2 text-xs', result.ok ? 'border-emerald-300 bg-emerald-50 text-emerald-900 dark:bg-emerald-950/20 dark:text-emerald-100' : 'border-destructive/30 bg-destructive/10 text-destructive')}>
      <div className="font-medium">{label}{result.ok ? '成功' : '失败'}</div>
      <div className="mt-1 grid gap-0.5 text-muted-foreground">
        {result.ip ? <span>出口 IP：{result.ip}</span> : null}
        {typeof result.elapsedMs === 'number' ? <span>耗时：{result.elapsedMs}ms</span> : null}
        {result.error ? <span className="break-words">{result.error}</span> : null}
      </div>
    </div>
  );
}

type SmsProviderPickerProps = {
  disabled?: boolean;
  onChange: (value: SmsProviderName[]) => void;
  onOpenChange: (open: boolean) => void;
  open: boolean;
  value: SmsProviderName[];
};

function SmsProviderPicker({ disabled, onChange, onOpenChange, open, value }: SmsProviderPickerProps) {
  const selected = normalizeSmsProviderSelection(value);
  const selectedSet = new Set(selected);

  function toggleProvider(provider: SmsProviderName) {
    const next = new Set(selected);
    if (next.has(provider)) {
      if (next.size <= 1) return;
      next.delete(provider);
    } else {
      next.add(provider);
    }
    onChange(SMS_PROVIDER_OPTIONS.map((option) => option.value).filter((providerName) => next.has(providerName)));
  }

  return (
    <div className="relative">
      <Button
        type="button"
        variant="outline"
        className="h-8 w-full justify-between px-2 font-normal"
        disabled={disabled}
        aria-expanded={open}
        title={`选择接码平台：${selected.join(', ')}`}
        onClick={() => onOpenChange(!open)}
      >
        <span className="min-w-0 truncate">{smsProviderSelectionLabel(selected)}</span>
        <ChevronDown size={14} className="ml-1 shrink-0" />
      </Button>
      {open && !disabled ? (
        <div className="absolute top-9 left-0 z-40 w-40 rounded-md border bg-popover p-1 text-popover-foreground shadow-md">
          {SMS_PROVIDER_OPTIONS.map((option) => (
            <button
              key={option.value}
              type="button"
              className="flex w-full items-center justify-between gap-3 rounded-md px-2 py-1.5 text-left text-sm transition hover:bg-muted"
              onClick={() => toggleProvider(option.value)}
            >
              <span>{option.label}</span>
              {selectedSet.has(option.value) ? <Check size={14} /> : <span className="size-3.5" />}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function normalizeSmsProviderSelection(value: SmsProviderName[]) {
  const selected = new Set(value);
  return SMS_PROVIDER_OPTIONS.map((option) => option.value).filter((provider) => selected.has(provider));
}

function smsProviderSelectionLabel(value: SmsProviderName[]) {
  if (value.length > 1) return `自动 · ${value.length}`;
  if (value.length === 0) return '请选择平台';
  return value.join(', ');
}

function emptySmsSettingsDraft(): SmsSettingsDraft {
  return {
    proxy: '',
    providers: SMS_PROVIDER_OPTIONS.map((option) => option.value),
    keys: { smsbower: '', herosms: '', '5sim': '' },
    clearKeys: { smsbower: false, herosms: false, '5sim': false },
  };
}

function smsSettingsDraftFromSettings(settings: SmsReceiverSettings): SmsSettingsDraft {
  const draft = emptySmsSettingsDraft();
  const providers = normalizeSmsProviderSelection((settings.providerOrder || []) as SmsProviderName[]);
  return {
    ...draft,
    proxy: settings.proxy || '',
    providers: providers.length ? providers : draft.providers,
  };
}

function smsSettingsProviderPayload(draft: SmsSettingsDraft): SmsReceiverSettingsUpdate['providers'] {
  const providerPayload: SmsReceiverSettingsUpdate['providers'] = {};
  for (const provider of SMS_PROVIDER_OPTIONS.map((option) => option.value)) {
    const apiKey = draft.keys[provider].trim();
    const clearApiKey = draft.clearKeys[provider];
    if (apiKey || clearApiKey) providerPayload[provider] = { apiKey, clearApiKey };
  }
  return providerPayload;
}

function smsSettingsDraftSignature(draft: SmsSettingsDraft) {
  return JSON.stringify({
    proxy: draft.proxy.trim(),
    providers: normalizeSmsProviderSelection(draft.providers),
    keys: SMS_PROVIDER_OPTIONS.map((option) => [option.value, draft.keys[option.value].trim()]),
    clearKeys: SMS_PROVIDER_OPTIONS.map((option) => [option.value, Boolean(draft.clearKeys[option.value])]),
  });
}

type SmsCountryPickerProps = {
  countries: SmsReceiverCountry[];
  disabled?: boolean;
  loading?: boolean;
  loadError?: string;
  onChange: (value: string) => void;
  onOpenChange: (open: boolean) => void;
  open: boolean;
  provider: SmsProviderOption;
  value: string;
};

function SmsCountryPicker({ countries, disabled, loading, loadError, onChange, onOpenChange, open, provider, value }: SmsCountryPickerProps) {
  const [query, setQuery] = useState('');
  const [manualText, setManualText] = useState('');
  const [manualDirty, setManualDirty] = useState(false);
  const selectedValues = parseSmsCountryValues(value, provider);
  const selectedSet = new Set(selectedValues.map(String));
  const visibleCountries = countries.filter((country) => smsCountryMatches(country, query));
  const displayValue = formatSelectedCountryNames(selectedValues, countries);
  const selectedTitle = formatSelectedCountryNames(selectedValues, countries, { full: true });
  const inputValue = manualDirty ? manualText : displayValue;

  function commitValues(values: Array<number | string>) {
    setManualDirty(false);
    setManualText('');
    setQuery('');
    onChange(formatSmsCountryValues(values));
  }

  function toggleCountryValue(countryValue: number | string) {
    const key = String(countryValue);
    if (selectedSet.has(key)) {
      commitValues(selectedValues.filter((selectedValue) => String(selectedValue) !== key));
      return;
    }
    commitValues([countryValue, ...selectedValues]);
  }

  function normalizeManualInput(manualValue: string) {
    if (!manualDirty) return;
    const normalized = formatSmsCountryValues(parseManualSmsCountryValues(manualValue, provider, countries));
    if (normalized || !manualValue.trim()) onChange(normalized);
    setManualDirty(false);
    setManualText('');
  }

  return (
    <div className="relative">
      <div className="flex min-w-0 gap-0 rounded-md focus-within:ring-[3px] focus-within:ring-ring/50">
        <Input
          className="min-w-0 flex-1 rounded-r-none border-r-0 focus-visible:ring-0"
          placeholder="哥伦比亚, 智利, 英格兰"
          value={inputValue}
          title={selectedTitle}
          onBlur={(event) => normalizeManualInput(event.currentTarget.value)}
          onChange={(event) => {
            setManualDirty(true);
            setManualText(event.target.value);
          }}
          disabled={disabled}
        />
        <Button
          type="button"
          size="icon"
          variant="outline"
          className="shrink-0 rounded-l-none focus-visible:ring-0"
          disabled={disabled}
          aria-expanded={open}
          aria-label={provider === '5sim' ? '选择国家' : '选择国家 ID'}
          title={provider === '5sim' ? '选择国家' : '选择国家 ID'}
          onClick={() => onOpenChange(!open)}
        >
          <ChevronDown size={14} />
        </Button>
      </div>
      {open && !disabled ? (
        <div className="absolute top-10 left-0 z-50 w-full min-w-[min(100%,18rem)] rounded-lg border bg-popover p-1 text-popover-foreground shadow-lg sm:w-[min(34rem,calc(100vw-7rem))]">
          <div className="sticky top-0 z-10 bg-popover p-1">
            <Input
              className="h-8"
              placeholder="搜索中文、英文、ID 或 slug"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
            {selectedValues.length ? <div className="mt-1 truncate px-1 text-[11px] text-muted-foreground" title={selectedTitle}>已选：{selectedTitle}</div> : null}
          </div>
          <div className="thin-scrollbar max-h-72 overflow-y-auto">
            {loadError ? <div className="px-2 py-1.5 text-xs text-muted-foreground">国家列表部分加载失败：{loadError}</div> : null}
            {loading ? <div className="px-2 py-1.5 text-xs text-muted-foreground">正在加载国家列表</div> : null}
            {visibleCountries.map((country) => {
              const countryValue = smsCountryValue(country);
              if (countryValue === null) return null;
              const selected = selectedSet.has(String(countryValue));
              return (
                <button
                  key={`${country.provider || 'country'}-${String(countryValue)}`}
                  type="button"
                  className={cn(
                    'grid w-full grid-cols-[minmax(9rem,1fr)_minmax(10rem,auto)] items-center gap-3 rounded-md px-2 py-1.5 text-left text-sm transition hover:bg-muted',
                    selected && 'bg-primary/10 text-primary hover:bg-primary/15'
                  )}
                  onClick={() => toggleCountryValue(countryValue)}
                >
                  <span className="min-w-0 truncate">{smsCountryLabel(country)}</span>
                  <span className="flex min-w-0 items-center justify-end gap-2 text-xs text-muted-foreground">
                    <span className="min-w-0 truncate">{smsCountryMeta(country)}</span>
                    {selected ? <Check size={14} className="text-foreground" /> : null}
                  </span>
                </button>
              );
            })}
            {!loading && visibleCountries.length === 0 ? <div className="px-2 py-2 text-sm text-muted-foreground">没有匹配国家</div> : null}
          </div>
        </div>
      ) : null}
    </div>
  );
}

function parseSmsCountryValues(value: string, provider: SmsProviderOption): Array<number | string> {
  const parts = value.split(/[,，、\s]+/).map((item) => item.trim()).filter(Boolean);
  const unified = parts.filter((item) => /^country:[a-z0-9_-]+$/i.test(item)).map((item) => `country:${item.split(':', 2)[1].toLowerCase()}`);
  const slugs = parts.map((item) => normalizeCountrySlug(item)).filter((item) => /^[a-z][a-z0-9_-]*$/.test(item) && item !== 'country');
  if (unified.length) return uniqueSmsCountryValues([...unified, ...slugs.map((slug) => `country:${slug}`)]);
  if (provider === '5sim') return uniqueSmsCountryValues(slugs);
  const ids = parts.filter((item) => /^\d+$/.test(item)).map((item) => Number.parseInt(item, 10));
  if (provider === '') return uniqueSmsCountryValues([...ids, ...slugs]);
  return uniqueSmsCountryValues(ids);
}

function parseManualSmsCountryValues(value: string, provider: SmsProviderOption, countries: SmsReceiverCountry[]) {
  const parts = value.split(/[,，、]+/).map((item) => item.trim()).filter(Boolean);
  const values: Array<number | string> = [];
  for (const part of parts) {
    const matched = countries.find((country) => smsCountryMatchesExact(country, part));
    const matchedValue = matched ? smsCountryValue(matched) : null;
    if (matchedValue !== null) {
      values.push(matchedValue);
      continue;
    }
    const direct = parseSmsCountryValues(part, '');
    if (direct.length) values.push(...direct);
  }
  return uniqueSmsCountryValues(values);
}

function uniqueSmsCountryValues(values: Array<number | string>) {
  const seen = new Set<string>();
  return values.filter((value) => {
    const key = String(value).trim();
    if (!key || seen.has(key)) return false;
    if (typeof value === 'number' && (!Number.isInteger(value) || value <= 0)) return false;
    seen.add(key);
    return true;
  });
}

function formatSmsCountryValues(values: Array<number | string>) {
  return uniqueSmsCountryValues(values).join(',');
}

function mergeSmsCountryOptions(primary: SmsReceiverCountry[], fallback: SmsReceiverCountry[]) {
  const byKey = new Map<string, SmsReceiverCountry>();
  [...fallback, ...primary].forEach((country) => {
    const value = smsCountryValue(country);
    if (value === null) return;
    const key = `${country.provider || 'country'}:${String(value)}`;
    const previous = byKey.get(key);
    byKey.set(key, {
      ...previous,
      ...country,
      name: country.name?.trim() || previous?.name || '',
    });
  });
  return [...byKey.values()].sort((left, right) => {
    const leftValue = smsCountryValue(left);
    const rightValue = smsCountryValue(right);
    const leftName = left.name?.trim() || `zz-${String(leftValue)}`;
    const rightName = right.name?.trim() || `zz-${String(rightValue)}`;
    return leftName.localeCompare(rightName, undefined, { sensitivity: 'base' }) || String(leftValue).localeCompare(String(rightValue));
  });
}

function smsCountryValue(country: SmsReceiverCountry): number | string | null {
  if (typeof country.value === 'string' && country.value.trim()) return country.value.trim();
  if (Number.isInteger(country.id) && (country.id || 0) > 0) return country.id as number;
  return null;
}

function smsCountryLabel(country: SmsReceiverCountry) {
  const name = localizedCountryName(country);
  const value = smsCountryValue(country);
  if (typeof value === 'string' && value.startsWith('country:')) return name || countryNameFromValue(value);
  if (typeof value === 'string') return name ? `${name} · ${value}` : value;
  return name ? `${name} · ID ${value}` : `国家 ID ${value}`;
}

function smsCountryMeta(country: SmsReceiverCountry) {
  const providers = country.providers || {};
  const providerParts = [
    providerMeta('smsbower', providers.smsbower),
    providerMeta('herosms', providers.herosms),
    providerMeta('5sim', providers['5sim']),
  ].filter(Boolean);
  if (providerParts.length) return providerParts.join(' · ');
  const value = smsCountryValue(country);
  const parts = [typeof value === 'string' ? value : `ID ${value}`];
  if (country.count && country.count > 0) parts.push(`${country.count} 个`);
  if (country.cost && country.cost > 0) parts.push(`$${country.cost.toFixed(3)}`);
  return parts.join(' · ');
}

function providerMeta(providerName: string, value: number | string | undefined) {
  if (value === undefined || value === null || value === '') return '';
  return providerName === '5sim' ? `5sim: ${value}` : `${providerName}: ID ${value}`;
}

function countryNameFromValue(value: string) {
  const slug = value.replace(/^country:/, '');
  const names = countryNamesBySlug.get(normalizeCountrySlug(slug));
  if (names) return names.display;
  return countryTitleFromSlug(slug);
}

function countryNameFromCallingCode(value: string) {
  const callingCode = value.replace(/\D+/g, '');
  if (!callingCode) return '';
  return countryNamesByCallingCode.get(callingCode) || '';
}

function formatSelectedCountryNames(values: Array<number | string>, countries: SmsReceiverCountry[], options: { full?: boolean } = {}) {
  const labels = values.map((value) => {
    const match = countries.find((country) => String(smsCountryValue(country)) === String(value));
    return match ? smsCountryShortLabel(match) : countryNameFromValue(String(value));
  });
  if (options.full || labels.length <= 3) return labels.join(', ');
  return `已选 ${labels.length} 个：${labels.slice(0, 3).join(', ')}...`;
}

function smsCountryMatches(country: SmsReceiverCountry, query: string) {
  const needle = query.trim().toLowerCase();
  if (!needle) return true;
  return [
    country.provider,
    country.name,
    country.id ? String(country.id) : '',
    country.value,
    country.providers?.smsbower,
    country.providers?.herosms,
    country.providers?.['5sim'],
    smsCountryLabel(country),
    smsCountryMeta(country),
    localizedCountrySearchText(country),
  ].some((value) => String(value || '').toLowerCase().includes(needle));
}

function smsCountryMatchesExact(country: SmsReceiverCountry, query: string) {
  const needle = query.trim().toLowerCase();
  const normalizedNeedle = normalizeCountrySlug(query);
  if (!needle) return false;
  return [
    country.name,
    country.value,
    country.id ? String(country.id) : '',
    country.providers?.smsbower,
    country.providers?.herosms,
    country.providers?.['5sim'],
    smsCountryLabel(country),
    smsCountryShortLabel(country),
    localizedCountrySearchText(country),
  ].some((value) => {
    const text = String(value || '').trim().toLowerCase();
    return text === needle || Boolean(normalizedNeedle) && normalizeCountrySlug(text) === normalizedNeedle;
  });
}

function normalizeCountrySlug(value: string) {
  return value.trim().toLowerCase().replace(/^country:/, '').replace(/&/g, 'and').replace(/[^a-z0-9]+/g, '');
}

function buildCountryNamesBySlug() {
  const map = new Map<string, { display: string; en: string; zh: string }>();
  for (const iso2 of getCountries()) {
    const zh = countryNameFormatterZh?.of(iso2) || '';
    const en = countryNameFormatterEn?.of(iso2) || '';
    const display = zh && en && zh.toLowerCase() !== en.toLowerCase() ? `${zh} ${en}` : zh || en || iso2;
    for (const name of [zh, en, display]) {
      const key = normalizeCountrySlug(name);
      if (key) map.set(key, { display, en, zh });
    }
  }
  map.set('england', { display: '英国 United Kingdom', en: 'United Kingdom', zh: '英国' });
  map.set('usa', { display: '美国 United States', en: 'United States', zh: '美国' });
  map.set('southafrica', { display: '南非 South Africa', en: 'South Africa', zh: '南非' });
  return map;
}

function buildCountryNamesByCallingCode() {
  const map = new Map<string, string>();
  for (const iso2 of getCountries()) {
    const code = getCountryCallingCode(iso2);
    if (!code || map.has(code)) continue;
    const zh = countryNameFormatterZh?.of(iso2) || '';
    const en = countryNameFormatterEn?.of(iso2) || '';
    map.set(code, zh || en || iso2);
  }
  map.set('1', '美国');
  map.set('44', '英国');
  return map;
}

function localizedCountryName(country: SmsReceiverCountry) {
  const rawName = country.name?.trim() || '';
  const value = smsCountryValue(country);
  const valueText = typeof value === 'string' ? value.replace(/^country:/, '') : '';
  const candidates = [rawName, valueText, country.providers?.['5sim']].map((candidate) => String(candidate || '').trim()).filter(Boolean);
  for (const candidate of candidates) {
    const names = countryNamesBySlug.get(normalizeCountrySlug(candidate));
    if (names) return names.display;
  }
  return rawName || countryNameFromValue(valueText);
}

function smsCountryShortLabel(country: SmsReceiverCountry) {
  const label = localizedCountryName(country);
  const zh = label.split(' ')[0] || label;
  return zh || label;
}

function localizedCountrySearchText(country: SmsReceiverCountry) {
  const label = localizedCountryName(country);
  const value = smsCountryValue(country);
  const names = countryNamesBySlug.get(normalizeCountrySlug(label));
  return [label, names?.zh, names?.en, value].filter(Boolean).join(' ');
}

function countryTitleFromSlug(slug: string) {
  return slug.split(/[-_]/).map((part) => part ? `${part[0]?.toUpperCase()}${part.slice(1)}` : '').join(' ');
}

function smsReceiverWaitSeconds(value: string) {
  const seconds = Number.parseInt(value.trim(), 10);
  return Number.isFinite(seconds) && seconds > 0 ? seconds : 180;
}

function smsCountryProviderErrorsMessage(errors?: Record<string, string>) {
  if (!errors) return '';
  const entries = Object.entries(errors).filter(([, message]) => String(message || '').trim());
  if (!entries.length) return '';
  return entries.map(([provider, message]) => `${provider}: ${smsCountryReadableError(message)}`).join('；');
}

function smsCountryShouldFallbackToDirect(result: SmsReceiverCountriesResult) {
  const countries = Array.isArray(result.countries) ? result.countries : [];
  if (!countries.length) return true;
  return smsCountryErrorMessages(result).some(smsCountryLooksLikeProxyError);
}

function smsCountryErrorMessages(result: SmsReceiverCountriesResult) {
  return [result.error_message, ...Object.values(result.errors || {})].map((message) => String(message || '').trim()).filter(Boolean);
}

function smsCountryLooksLikeProxyError(message: string) {
  const text = message.toLowerCase();
  return ['connection refused', 'urlopen error', 'proxy', 'connect failed', 'connection reset', 'no route to host', 'network is unreachable', 'timed out', 'timeout', 'deadline', '代理连接失败', '连接失败'].some((marker) => text.includes(marker));
}

function smsCountryReadableError(message: unknown) {
  const text = String(message || '').trim();
  if (!text) return '未知错误';
  if (smsCountryLooksLikeProxyError(text)) return '代理连接失败';
  if (/unauthorized|forbidden|invalid.*key|invalid.*token|api.?key|token|401|403/i.test(text)) return 'Key 可能无效';
  if (/not found|404/i.test(text)) return '平台接口不可用';
  return text.length > 120 ? `${text.slice(0, 117)}...` : text;
}

function smsCountryLoadErrorMessage(message: unknown) {
  const text = String(message || '').trim();
  if (!text) return '国家列表加载失败，请检查接码平台 Key 或网络';
  if (smsCountryLooksLikeProxyError(text)) return '配置代理不可用，直连加载也失败，请检查代理或服务器网络';
  return smsCountryReadableError(text);
}

function isAbortError(error: unknown) {
  return error instanceof DOMException && error.name === 'AbortError';
}

function accountAddBadgeLabel(pending: boolean, blocked: boolean, canRegister: boolean, cooldownSeconds: number, detected: boolean) {
  if (pending) return '等待 OTP';
  if (blocked) return '已封禁';
  if (canRegister) return '可注册';
  if (cooldownSeconds > 0) return `冷却 ${countdownLabel(cooldownSeconds)}`;
  if (detected) return '暂无可用';
  return '待检测';
}

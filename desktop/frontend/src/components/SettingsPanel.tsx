import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { QRCodeSVG } from "qrcode.react";
import { Check, CheckCircle2, ChevronDown, Loader2, QrCode, RefreshCw, Trash2 } from "lucide-react";
import { asArray } from "../lib/array";
import { useDeferredClose } from "../lib/useMountTransition";
import { app } from "../lib/bridge";
import { normalizeLangPref, useI18n, useT, type DictKey, type LangPref } from "../lib/i18n";
import { mergedFetchedProviderModels, providerDefaultModel, providerModelCandidates } from "../lib/providerModels";
import { useUpdater } from "../lib/useUpdater";
import {
  THEME_STYLES,
  applyTheme,
  getTheme,
  getThemeStyle,
  normalizeThemePreference,
  normalizeThemeStyleForTheme,
  type Theme,
  type ThemeStyle,
} from "../lib/theme";
import { TEXT_SIZES, applyTextSize, getTextSize, type TextSize } from "../lib/textSize";
import { FONT_FAMILIES, applyFontFamily, getFontFamily, type FontFamily } from "../lib/fontFamily";
import { getDisplayMode, onDisplayModeChange, setDisplayMode as setLocalDisplayMode } from "../lib/displayMode";
import type { BotConnectionView, BotInstallStartResult, BotSettingsView, HookConfigView, HooksSettingsView, MailProbeResult, NetworkView, ProviderView, SettingsTab, SettingsView } from "../lib/types";
import { InlineConfirmButton } from "./InlineConfirmButton";
import { Tooltip } from "./Tooltip";
import { AnchoredPopover } from "./AnchoredPopover";
import { MCPServersSettingsPage, SkillsSettingsPage } from "./CapabilitiesPanel";
import { MemorySettingsPage } from "./MemoryPanel";
import { ModalCloseButton } from "./ModalCloseButton";

const SETTINGS_TABS: SettingsTab[] = ["general", "models", "bots", "cowork", "mcp", "skills", "memory", "permissions", "sandbox", "network", "hooks", "appearance", "updates"];

// SettingsPanel is the desktop settings centre — a centred modal with left
// navigation and a right content area. It hosts all settings pages plus MCP,
// Skills, and Memory management, replacing the old per-feature drawers.
export function SettingsPanel({ onClose, onChanged, initialTab, initialPayload }: { onClose: () => void; onChanged: () => void; initialTab?: SettingsTab; initialPayload?: string }) {
  const t = useT();
  const [s, setS] = useState<SettingsView | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [theme, setThemeState] = useState<Theme>(getTheme());
  const [themeStyle, setThemeStyleState] = useState<ThemeStyle>(() => getThemeStyle(getTheme()));
  const [textSize, setTextSizeState] = useState<TextSize>(getTextSize());
  const [fontFamily, setFontFamilyState] = useState<FontFamily>(getFontFamily());
  const [tab, setTab] = useState<SettingsTab>(initialTab === "providers" ? "models" : initialTab ?? "general");
  // Play the modal exit animation, then let the parent unmount us.
  const { status, requestClose } = useDeferredClose(onClose, 240);

  const reload = async () => setS(normalizeSettingsView(await app.Settings().catch(() => null)));
  useEffect(() => {
    void reload();
    if (initialTab) setTab(initialTab === "providers" ? "models" : initialTab);
  }, [initialTab]);
  useEffect(() => {
    if (!s) return;
    const nextTheme = normalizeThemePreference(s.desktopTheme);
    const nextStyle = normalizeThemeStyleForTheme(s.desktopThemeStyle, nextTheme);
    setThemeState(nextTheme);
    setThemeStyleState(nextStyle);
  }, [s?.desktopTheme, s?.desktopThemeStyle]);

  // apply runs a mutation, re-reads settings, and refreshes the topbar/model.
  const apply = async (fn: () => Promise<void>) => {
    setBusy(true);
    setErr(null);
    try {
      await fn();
      await reload();
      onChanged();
    } catch (e) {
      setErr(String((e as Error)?.message ?? e));
    } finally {
      setBusy(false);
    }
  };
  const backgroundApply = async (fn: () => Promise<void>) => {
    setErr(null);
    try {
      await fn();
      await reload();
      onChanged();
    } catch (e) {
      setErr(String((e as Error)?.message ?? e));
    }
  };

  // Close on Esc
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !document.querySelector("[data-anchored-popover='active']")) requestClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [requestClose]);

  // The settings-reliant pages (general, models, network, permissions,
  // sandbox, appearance, updates) need SettingsView loaded. MCP, Skills, and Memory
  // load their own data and render regardless.
  const needsSettings = tab === "general" || tab === "models" || tab === "bots" || tab === "cowork" || tab === "network" || tab === "permissions" || tab === "sandbox" || tab === "appearance" || tab === "updates";

  return (
    <div className="management-modal-backdrop settings-modal-backdrop" data-state={status} onClick={(e) => { if (e.target === e.currentTarget) requestClose(); }}>
      <div className="management-modal settings-modal" data-state={status}>
        <header className="management-modal__head settings-modal__head">
          <div className="management-modal__title settings-modal__title">{t("settings.title")}</div>
          <ModalCloseButton label={t("common.close")} onClick={requestClose} />
        </header>

        <div className="settings-center">
          <nav className="settings-center__nav" aria-label={t("settings.title")}>
            {SETTINGS_TABS.map((id) => (
              <button
                key={id}
                className={`settings-center__navitem${tab === id ? " settings-center__navitem--active" : ""}`}
                onClick={() => setTab(id)}
              >
                <span>{settingsTabLabel(id, t)}</span>
                {s && <small>{settingsTabMeta(id, s, t)}</small>}
              </button>
            ))}
          </nav>
          <main className="settings-center__content">
            {needsSettings && err && <div className="banner banner--error">{err}</div>}
            {needsSettings && !s ? (
              <div className="empty">{t("settings.loading")}</div>
            ) : (
              <>
                {tab === "general" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><GeneralSection s={s} busy={busy} apply={apply} /></SettingsPageShell>}
                {tab === "models" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><ModelsSection s={s} busy={busy} apply={apply} backgroundApply={backgroundApply} /></SettingsPageShell>}
                {tab === "bots" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><BotsSection s={s} busy={busy} apply={apply} /></SettingsPageShell>}
                {tab === "cowork" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><CoWorkSection s={s} busy={busy} apply={apply} /></SettingsPageShell>}
                {tab === "mcp" && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy ?? false} apply={apply}><MCPServersSettingsPage initialHighlight={initialPayload} />{s && <WebSearchSection s={s} busy={busy} apply={apply} />}</SettingsPageShell>}
                {tab === "skills" && <SettingsPageShell key={tab} s={s} tab={tab} busy={false} apply={apply}><SkillsSettingsPage initialHighlight={initialPayload} /><JiutianSection /></SettingsPageShell>}
                {tab === "memory" && <SettingsPageShell key={tab} s={s} tab={tab} busy={false} apply={apply}><MemorySettingsPage /></SettingsPageShell>}
                {tab === "permissions" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><PermissionsSection s={s} busy={busy} apply={apply} /></SettingsPageShell>}
                {tab === "sandbox" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><SandboxSection s={s} busy={busy} apply={apply} /></SettingsPageShell>}
                {tab === "network" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><NetworkSection s={s} busy={busy} apply={apply} /></SettingsPageShell>}
                {tab === "hooks" && <SettingsPageShell key={tab} s={s} tab={tab} busy={false} apply={apply}><HooksSection onChanged={() => onChanged()} /></SettingsPageShell>}
                {tab === "appearance" && s && (
                  <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}>
                    <AppearanceSection
                      theme={theme}
                      themeStyle={themeStyle}
                      textSize={textSize}
                      fontFamily={fontFamily}
                      onTheme={(nextTheme) => {
                        applyTheme(nextTheme, themeStyle, { persist: false });
                        setThemeState(nextTheme);
                        void apply(() => app.SetDesktopAppearance(nextTheme, themeStyle));
                      }}
                      onThemeStyle={(style) => {
                        applyTheme(theme, style, { persist: false });
                        setThemeStyleState(style);
                        void apply(() => app.SetDesktopAppearance(theme, style));
                      }}
                      onTextSize={(size) => {
                        applyTextSize(size);
                        setTextSizeState(size);
                      }}
                      onFontFamily={(font) => {
                        applyFontFamily(font);
                        setFontFamilyState(font);
                      }}
                    />
                  </SettingsPageShell>
                )}
                {tab === "updates" && s && (
                  <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}>
                    <UpdatesSection
                      configPath={s.configPath}
                      checkUpdates={s.checkUpdates}
                      telemetry={s.telemetry !== false}
                      settingsBusy={busy}
                      applySettings={apply}
                    />
                  </SettingsPageShell>
                )}
              </>
            )}
          </main>
        </div>
      </div>
    </div>
  );
}

function SettingsPageShell({ s: _s, tab, children }: { s: SettingsView | null; tab: SettingsTab; busy: boolean; apply: (fn: () => Promise<void>) => Promise<void>; children: ReactNode }) {
  const t = useT();
  const descKey = `settings.pageDesc.${tab}` as keyof typeof import("../locales/en").en;
  const desc = t(descKey as any);
  return (
    <div className={`settings-page settings-page--${settingsPageKind(tab)}`}>
      <div className="settings-page__header">
        <h2 className="settings-page__title">{settingsTabPageTitle(tab, t)}</h2>
        {typeof desc === "string" && desc !== `settings.pageDesc.${tab}` && <p className="settings-page__desc">{desc}</p>}
      </div>
      {children}
    </div>
  );
}

function settingsPageKind(tab: SettingsTab): "form" | "manager" {
  switch (tab) {
    case "models":
    case "mcp":
    case "skills":
    case "memory":
      return "manager";
    default:
      return "form";
  }
}

function SettingsSection({
  title,
  description,
  actions,
  children,
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="settings-section">
      <div className="settings-section__head">
        <div>
          <div className="settings-section__title">{title}</div>
          {description && <div className="settings-section__desc">{description}</div>}
        </div>
        {actions && <div className="settings-section__actions">{actions}</div>}
      </div>
      <div className="settings-section__body">{children}</div>
    </section>
  );
}

function SettingsField({
  label,
  hint,
  children,
  className,
  stacked = false,
}: {
  label: ReactNode;
  hint?: ReactNode;
  children: ReactNode;
  className?: string;
  stacked?: boolean;
}) {
  return (
    <div className={`settings-field${stacked || hint ? " settings-field--stacked" : ""}${className ? ` ${className}` : ""}`}>
      <div className="settings-field__copy">
        <div className="settings-field__label">{label}</div>
        {hint && (
          <div className="settings-field__hint">
            <SettingsHint hint={hint} />
          </div>
        )}
      </div>
      <div className="settings-field__control">{children}</div>
    </div>
  );
}

function SettingsHint({ hint }: { hint: ReactNode }) {
  if (typeof hint === "string" || typeof hint === "number") {
    const label = String(hint);
    return (
      <Tooltip label={label} fill block className="settings-field__hint-tooltip">
        <span className="settings-field__hint-line">{label}</span>
      </Tooltip>
    );
  }
  return hint;
}

function settingsTabPageTitle(id: SettingsTab, t: ReturnType<typeof useT>): string {
  switch (id) {
    case "mcp": return t("settings.tab.mcp");
    case "skills": return t("settings.tab.skills");
    case "memory": return t("settings.tab.memory");
    default: return settingsTabLabel(id, t);
  }
}

type SectionProps = {
  s: SettingsView;
  busy: boolean;
  apply: (fn: () => Promise<void>) => Promise<void>;
};

type ModelsSectionProps = SectionProps & {
  backgroundApply: (fn: () => Promise<void>) => Promise<void>;
};

function settingsTabLabel(id: SettingsTab, t: ReturnType<typeof useT>): string {
  switch (id) {
    case "general":
      return t("settings.tab.general");
    case "models":
      return t("settings.tab.models");
    case "providers":
      return t("settings.tab.providers");
    case "bots":
      return t("settings.tab.bots");
    case "mcp":
      return t("settings.tab.mcp");
    case "skills":
      return t("settings.tab.skills");
    case "memory":
      return t("settings.tab.memory");
    case "network":
      return t("settings.tab.network");
    case "hooks":
      return t("settings.tab.hooks");
    case "permissions":
      return t("settings.tab.permissions");
    case "sandbox":
      return t("settings.tab.sandbox");
    case "appearance":
      return t("settings.tab.appearance");
    case "updates":
      return t("settings.tab.updates");
    case "cowork":
      return t("settings.tab.cowork");
    default:
      return id;
  }
}

function settingsTabMeta(id: SettingsTab, s: SettingsView, t: ReturnType<typeof useT>): string {
  switch (id) {
    case "models":
      return settingsModelMeta(s, t);
    case "general":
      return `${closeBehaviorLabel(normalizeCloseBehavior(s.closeBehavior), t)} · ${t(`settings.autoPlan.${normalizeAutoPlan(s.autoPlan)}`)}`;
    case "providers":
      return t("settings.providerCount", { n: s.providers.length });
    case "bots":
      return botSettingsMeta(s.bot, t);
    case "mcp":
      return t("caps.connectorsTab");
    case "skills":
      return t("caps.skillsTab");
    case "memory":
      return t("settings.tabSub.memory");
    case "network":
      return proxyModeLabel(normalizeProxyMode(s.network.proxyMode), t);
    case "hooks":
      return t("settings.tabSub.hooks");
    case "permissions":
      return permissionModeLabel(s.permissions.mode, t);
    case "sandbox":
      return sandboxModeLabel(s.sandbox.bash, t);
    case "appearance":
      return t("settings.appearanceMeta");
    case "updates":
      return t("settings.updatesMeta");
    case "cowork":
      return t("settings.tabSub.cowork");
    default:
      return "";
  }
}

function settingsModelMeta(s: SettingsView, t: ReturnType<typeof useT>): string {
  const ref = toRef(s.defaultModel, s);
  if (!ref) return t("common.none");
  if (!ref.includes("/")) return ref;
  const [provider, ...modelParts] = ref.split("/");
  const model = modelParts.join("/") || ref;
  const providerView = s.providers.find((p) => p.name === provider);
  return `${modelProviderLabel(provider, providerView, t)} · ${model}`;
}

function botSettingsMeta(bot: BotSettingsView, t: ReturnType<typeof useT>): string {
  const normalized = normalizeBotSettings(bot);
  const connections = normalized.connections.length;
  if (connections === 0) return t("settings.botNoConnections");
  if (!normalized.enabled) return t("settings.botDisabledWithConnections", { n: connections });
  return t("settings.botConnectionCount", { n: connections });
}

// allRefs flattens providers into "provider/model" refs for the model selectors.
function allRefs(s: SettingsView): string[] {
  const out: string[] = [];
  for (const p of s.providers) {
    if (!p.added || !p.keySet) continue;
    for (const m of p.models) out.push(`${p.name}/${m}`);
  }
  return out;
}

// toRef normalises a stored model id (a provider name, a bare model, or a ref) to
// a "provider/model" ref so a <select> of refs can show it selected.
function toRef(model: string, s: SettingsView): string {
  if (!model) return "";
  if (model.includes("/")) return model;
  const byName = s.providers.find((p) => p.name === model);
  if (byName) return `${byName.name}/${byName.default || byName.models[0] || ""}`;
  const byModel = s.providers.find((p) => p.models.includes(model));
  if (byModel) return `${byModel.name}/${model}`;
  return model;
}

const PROXY_MODES = ["auto", "custom", "off"] as const;

// EFFORT_PRESETS is the canonical union of /effort levels the kernel
// recognises. The settings UI exposes these as toggleable checkboxes; users
// can additionally add arbitrary custom names via the "Add" input. The order
// here is what the user sees in the dropdown.
const EFFORT_PRESETS: readonly string[] = ["low", "medium", "high", "xhigh", "max"];
const REASONING_PROTOCOLS: readonly string[] = ["", "moma", "MoMA", "openai", "none"];

// COMMON_PROVIDER_PRESETS are one-click base_url + context_window shortcuts for
// the most popular OpenAI-compatible platforms, so users don't have to look up
// and hand-type the API endpoint. Selecting a preset fills both fields; the
// user can then edit freely.
const COMMON_PROVIDER_PRESETS: { label: string; baseUrl: string; context: number }[] = [
  { label: "DeepSeek", baseUrl: "https://api.deepseek.com", context: 128000 },
  { label: "Kimi", baseUrl: "https://api.moonshot.cn/v1", context: 128000 },
  { label: "智谱", baseUrl: "https://open.bigmodel.cn/api/paas/v4", context: 128000 },
  { label: "通义", baseUrl: "https://dashscope.aliyuncs.com/compatible-mode/v1", context: 128000 },
  { label: "OpenRouter", baseUrl: "https://openrouter.ai/api/v1", context: 200000 },
];

const PROXY_TYPES = ["http", "https", "socks5", "socks5h"] as const;
const LANGUAGE_PREFS: LangPref[] = ["", "zh", "en"];
const AUTO_PLAN_MODES = ["off", "on"] as const;

type ProxyMode = (typeof PROXY_MODES)[number];
type AutoPlanMode = (typeof AUTO_PLAN_MODES)[number];

function normalizeProxyMode(mode: string): ProxyMode {
  switch (mode) {
    case "custom":
      return "custom";
    case "off":
      return "off";
    default:
      return "auto";
  }
}

function normalizeNetworkView(network: NetworkView): NetworkView {
  return { ...network, proxyMode: normalizeProxyMode(network.proxyMode) };
}

function normalizeAutoPlan(mode: string | undefined): AutoPlanMode {
  return mode === "ask" || mode === "on" ? "on" : "off";
}

function normalizeReasoningProtocol(protocol: string | undefined): string {
  return REASONING_PROTOCOLS.includes(protocol ?? "") ? protocol ?? "" : "";
}

function defaultBotSettings(): BotSettingsView {
  return {
    enabled: false,
    model: "",
    maxSteps: 0,
    debounceMs: 1500,
    allowlist: {
      enabled: true,
      allowAll: false,
      mode: "open",
      qqUsers: [],
      feishuUsers: [],
      weixinUsers: [],
      qqGroups: [],
      feishuGroups: [],
      weixinGroups: [],
    },
    qq: { enabled: false, appId: "", appSecretEnv: "QQ_BOT_APP_SECRET", secretSet: false },
    feishu: {
      enabled: false,
      domain: "feishu",
      appId: "",
      appSecretEnv: "FEISHU_BOT_APP_SECRET",
      secretSet: false,
      verificationToken: "",
      mode: "webhook",
      webhookPort: 8080,
      requireMention: true,
    },
    weixin: {
      enabled: false,
      accountId: "default",
      tokenEnv: "WEIXIN_BOT_TOKEN",
      tokenSet: false,
      apiBase: "https://ilinkai.weixin.qq.com",
    },
    connections: [],
  };
}

function normalizeBotSettings(bot: BotSettingsView | null | undefined): BotSettingsView {
  const fallback = defaultBotSettings();
  const allowlist = bot?.allowlist ?? fallback.allowlist;
  const mode = bot?.feishu?.mode === "websocket" ? "websocket" : "webhook";
  return {
    ...fallback,
    ...bot,
    maxSteps: Math.max(0, Number(bot?.maxSteps ?? fallback.maxSteps) || 0),
    debounceMs: Number(bot?.debounceMs) || fallback.debounceMs,
    allowlist: {
      ...fallback.allowlist,
      ...allowlist,
      qqUsers: asArray(allowlist.qqUsers),
      feishuUsers: asArray(allowlist.feishuUsers),
      weixinUsers: asArray(allowlist.weixinUsers),
      qqGroups: asArray(allowlist.qqGroups),
      feishuGroups: asArray(allowlist.feishuGroups),
      weixinGroups: asArray(allowlist.weixinGroups),
    },
    qq: { ...fallback.qq, ...bot?.qq },
    feishu: { ...fallback.feishu, ...bot?.feishu, domain: bot?.feishu?.domain === "lark" ? "lark" : "feishu", mode },
    weixin: { ...fallback.weixin, ...bot?.weixin },
    connections: asArray(bot?.connections).map(normalizeBotConnection),
  };
}

function normalizeBotConnection(raw: any) {
  const credential = raw?.credential ?? {};
  const workspaceRoot = String(raw?.workspaceRoot ?? "").trim();
  return {
    id: String(raw?.id ?? "").trim(),
    provider: String(raw?.provider ?? "").trim(),
    domain: String(raw?.domain ?? "").trim(),
    label: String(raw?.label ?? "").trim(),
    enabled: raw?.enabled !== false,
    status: String(raw?.status ?? "disconnected").trim(),
    model: String(raw?.model ?? "").trim(),
    workspaceRoot,
    credential: {
      appId: String(credential.appId ?? "").trim(),
      appSecretEnv: String(credential.appSecretEnv ?? "").trim(),
      accountId: String(credential.accountId ?? "").trim(),
      tokenEnv: String(credential.tokenEnv ?? "").trim(),
      secretSet: Boolean(credential.secretSet),
    },
    sessionMappings: asArray(raw?.sessionMappings).map((item: any) => ({
      remoteId: String(item?.remoteId ?? "").trim(),
      sessionId: String(item?.sessionId ?? "").trim(),
      scope: normalizeBotMappingScope(item?.scope, item?.workspaceRoot ?? workspaceRoot),
      workspaceRoot: normalizeBotMappingScope(item?.scope, item?.workspaceRoot ?? workspaceRoot) === "project"
        ? String(item?.workspaceRoot ?? workspaceRoot).trim()
        : "",
      updatedAt: String(item?.updatedAt ?? "").trim(),
    })),
    lastError: String(raw?.lastError ?? "").trim(),
    createdAt: String(raw?.createdAt ?? "").trim(),
    updatedAt: String(raw?.updatedAt ?? "").trim(),
  };
}

function normalizeBotMappingScope(scope: unknown, workspaceRoot: unknown): "global" | "project" {
  if (String(scope ?? "").trim() === "project") return "project";
  return String(workspaceRoot ?? "").trim() ? "project" : "global";
}

function normalizeSettingsView(view: SettingsView | null | undefined): SettingsView | null {
  if (!view) return null;
  const permissions = view.permissions ?? { mode: "ask", allow: [], ask: [], deny: [] };
  const sandbox = view.sandbox ?? { bash: "enforce", network: false, workspaceRoot: "", allowWrite: [] };
  const network = view.network ?? {
    proxyMode: "auto",
    proxyUrl: "",
    noProxy: "",
    proxy: { type: "socks5", server: "", port: 0, username: "", password: "" },
  };
  const agent = view.agent ?? { temperature: 0, maxSteps: 0, systemPrompt: "" };
  agent.maxSteps = Number.isFinite(agent.maxSteps) ? Math.max(0, Math.trunc(agent.maxSteps)) : 0;
  return {
    ...view,
    providers: asArray(view.providers).map((p) => ({
      ...p,
      builtIn: Boolean(p.builtIn),
      added: Boolean(p.added),
      models: asArray(p.models),
      modelsUrl: p.modelsUrl ?? "",
      reasoningProtocol: normalizeReasoningProtocol(p.reasoningProtocol),
      supportedEfforts: asArray(p.supportedEfforts),
    })),
    providerKinds: asArray(view.providerKinds),
    permissions: {
      ...permissions,
      allow: asArray(permissions.allow),
      ask: asArray(permissions.ask),
      deny: asArray(permissions.deny),
    },
    sandbox: {
      ...sandbox,
      allowWrite: asArray(sandbox.allowWrite),
    },
    network: {
      ...network,
      proxy: network.proxy ?? { type: "socks5", server: "", port: 0, username: "", password: "" },
    },
    agent,
    bot: normalizeBotSettings(view.bot),
    autoPlan: normalizeAutoPlan(view.autoPlan),
    autoApproveTools: Boolean(view.autoApproveTools ?? view.bypass),
    bypass: Boolean(view.autoApproveTools ?? view.bypass),
    desktopLanguage: normalizeLangPref(view.desktopLanguage),
    desktopTheme: normalizeThemePreference(view.desktopTheme),
    desktopThemeStyle: normalizeThemeStyleForTheme(view.desktopThemeStyle, normalizeThemePreference(view.desktopTheme)),
    closeBehavior: normalizeCloseBehavior(view.closeBehavior),
    displayMode: normalizeDisplayMode(view.displayMode),
    checkUpdates: view.checkUpdates !== false,
  };
}

type CloseBehavior = "background" | "quit";

function normalizeCloseBehavior(mode: string | undefined): CloseBehavior {
  return mode === "quit" ? "quit" : "background";
}

type DisplayMode = "standard" | "compact" | "minimal";

function normalizeDisplayMode(mode: string | undefined): DisplayMode {
  return mode === "standard" || mode === "compact" || mode === "minimal" ? mode : "minimal";
}

function closeBehaviorLabel(mode: CloseBehavior, t: ReturnType<typeof useT>): string {
  return mode === "quit" ? t("settings.closeBehavior.quit") : t("settings.closeBehavior.background");
}

function permissionModeLabel(mode: string, t: ReturnType<typeof useT>): string {
  switch (mode) {
    case "allow":
      return t("settings.modeAllowShort");
    case "deny":
      return t("settings.modeDenyShort");
    default:
      return t("settings.modeAskShort");
  }
}

function sandboxModeLabel(mode: string, t: ReturnType<typeof useT>): string {
  return mode === "off" ? t("settings.bashOffShort") : t("settings.bashEnforceShort");
}

function reasoningProtocolLabel(protocol: string, t: ReturnType<typeof useT>): string {
  switch (protocol) {
    case "moma":
      return t("settings.reasoningProtocol.moma");
    case "MoMA":
      return t("settings.reasoningProtocol.moma");
    case "openai":
      return t("settings.reasoningProtocol.openai");
    case "none":
      return t("settings.reasoningProtocol.none");
    default:
      return t("settings.reasoningProtocol.auto");
  }
}

// effortLabel maps raw effort levels (low/medium/high/xhigh/max) to localized
// friendly names so users don't see raw English enum values in dropdowns and
// checkboxes. Falls back to the raw value if unrecognized.
export function effortLabel(level: string, t: ReturnType<typeof useI18n>["t"]): string {
  switch (level) {
    case "low":
      return t("settings.effortLow");
    case "medium":
      return t("settings.effortMedium");
    case "high":
      return t("settings.effortHigh");
    case "xhigh":
      return t("settings.effortXhigh");
    case "max":
      return t("settings.effortMax");
    default:
      return level;
  }
}

// protocolLabel maps provider "kind" values to friendly names for the protocol
// dropdown, hiding raw enum strings from end users.
export function protocolLabel(kind: string, t: ReturnType<typeof useI18n>["t"]): string {
  switch (kind) {
    case "openai":
      return t("settings.protocolOpenai");
    default:
      return kind;
  }
}

function GeneralSection({ s, busy, apply }: SectionProps) {
  const { t, setPref } = useI18n();
  const closeBehavior = normalizeCloseBehavior(s.closeBehavior);
  const [displayMode, setDisplayMode] = useState<DisplayMode>(() => normalizeDisplayMode(getDisplayMode()));
  useEffect(() => onDisplayModeChange((mode) => setDisplayMode(mode)), []);
  const autoPlan = normalizeAutoPlan(s.autoPlan);
  const languagePref = normalizeLangPref(s.desktopLanguage);
  const setLanguage = (next: LangPref) => {
    setPref(next);
    void apply(() => app.SetDesktopLanguage(next));
  };
  return (
    <SettingsSection title={t("settings.tab.general")}>
      <SettingsField label={t("settings.language")}>
        <div className="set-seg">
          {LANGUAGE_PREFS.map((pref) => (
            <button
              key={pref || "auto"}
              className={`set-seg__btn${languagePref === pref ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => setLanguage(pref)}
            >
              {pref === "" ? t("settings.langAuto") : pref === "zh" ? "中文" : "English"}
            </button>
          ))}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.closeBehavior")}>
        <div className="set-seg">
          {(["background", "quit"] as const).map((mode) => (
            <button
              key={mode}
              className={`set-seg__btn${closeBehavior === mode ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => void apply(() => app.SetCloseBehavior(mode))}
            >
              {closeBehaviorLabel(mode, t)}
            </button>
          ))}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.expandThinking")}>
        <div className="set-seg">
          {([false, true] as const).map((val) => (
            <button
              key={val ? "on" : "off"}
              className={`set-seg__btn${s.expandThinking === val ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => void apply(() => app.SetExpandThinking(val))}
            >
              {val ? t("settings.expandThinking.expanded") : t("settings.expandThinking.collapsed")}
            </button>
          ))}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.displayMode")}>
        <div className="set-seg">
          {(["standard", "compact", "minimal"] as const).map((mode) => (
            <button
              key={mode}
              className={`set-seg__btn${displayMode === mode ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => {
                setLocalDisplayMode(mode);
                void apply(() => app.SetDisplayMode(mode));
              }}
            >
              {t(`settings.displayMode.${mode}`)}
            </button>
          ))}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.autoPlan")}>
        <div className="set-seg">
          {AUTO_PLAN_MODES.map((mode) => (
            <button
              key={mode}
              className={`set-seg__btn${autoPlan === mode ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => void apply(() => app.SetAutoPlan(mode))}
            >
              {t(`settings.autoPlan.${mode}`)}
            </button>
          ))}
        </div>
      </SettingsField>
    </SettingsSection>
  );
}

function StepLimitControl({
  value,
  presets,
  busy,
  onChange,
}: {
  value: number;
  presets: number[];
  busy: boolean;
  onChange: (value: number) => void;
}) {
  const t = useT();
  const normalized = normalizeStepLimit(value);
  const presetSet = new Set(presets.map(normalizeStepLimit));
  const [custom, setCustom] = useState(String(normalized));
  useEffect(() => setCustom(String(normalized)), [normalized]);
  const isCustom = !presetSet.has(normalized);
  const commitCustom = () => {
    const next = normalizeStepLimit(Number(custom));
    setCustom(String(next));
    if (next !== normalized) onChange(next);
  };
  return (
    <div className="step-limit-control">
      <div className="set-seg">
        {presets.map((preset) => {
          const n = normalizeStepLimit(preset);
          return (
            <button
              key={n}
              type="button"
              className={`set-seg__btn${normalized === n ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => n !== normalized && onChange(n)}
            >
              {stepLimitLabel(n, t)}
            </button>
          );
        })}
        <button
          type="button"
          className={`set-seg__btn${isCustom ? " set-seg__btn--on" : ""}`}
          disabled={busy}
          onClick={() => {
            if (!isCustom) setCustom(String(normalized || 12));
          }}
        >
          {t("settings.stepLimit.custom")}
        </button>
      </div>
      <input
        className="mem-input step-limit-control__custom"
        value={custom}
        disabled={busy}
        inputMode="numeric"
        aria-label={t("settings.stepLimit.custom")}
        onChange={(e) => setCustom(e.target.value.replace(/[^\d]/g, ""))}
        onBlur={commitCustom}
        onKeyDown={(e) => {
          if (e.key === "Enter") e.currentTarget.blur();
        }}
      />
    </div>
  );
}

function normalizeStepLimit(value: number): number {
  return Number.isFinite(value) && value > 0 ? Math.trunc(value) : 0;
}

function stepLimitLabel(value: number, t: ReturnType<typeof useT>): string {
  return value === 0 ? t("settings.stepLimit.unlimited") : String(value);
}

function NetworkSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  const savedNetwork = normalizeNetworkView(s.network);
  const [draft, setDraft] = useState<NetworkView>(savedNetwork);
  useEffect(() => setDraft(normalizeNetworkView(s.network)), [s.network]);
  const dirty = JSON.stringify(draft) !== JSON.stringify(savedNetwork);
  const setProxy = (next: Partial<NetworkView["proxy"]>) => {
    setDraft({ ...draft, proxy: { ...draft.proxy, ...next } });
  };

  return (
    <SettingsSection
      title={t("settings.tab.network")}
      actions={
        <button
          className="btn btn--primary btn--small"
          disabled={busy || !dirty}
          onClick={() => void apply(() => app.SetNetwork(draft))}
        >
          {t("settings.saveNetwork")}
        </button>
      }
    >
      <SettingsField label={t("settings.proxyMode")}>
        <div className="set-seg">
          {PROXY_MODES.map((mode) => (
            <button
              key={mode}
              className={`set-seg__btn${draft.proxyMode === mode ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => setDraft({ ...draft, proxyMode: mode })}
            >
              {proxyModeLabel(mode, t)}
            </button>
          ))}
        </div>
      </SettingsField>

      {draft.proxyMode === "custom" && (
        <>
          <SettingsField label={t("settings.proxyType")}>
            <div className="set-seg">
              {PROXY_TYPES.map((typ) => (
                <button
                  key={typ}
                  className={`set-seg__btn${draft.proxy.type === typ ? " set-seg__btn--on" : ""}`}
                  disabled={busy}
                  onClick={() => setProxy({ type: typ })}
                >
                  {typ.toUpperCase()}
                </button>
              ))}
            </div>
          </SettingsField>
          <SettingsField label={t("settings.proxyServer")}>
            <div className="settings-inline-controls">
            <input
              className="mem-input set-grow"
              placeholder="127.0.0.1"
              value={draft.proxy.server}
              disabled={busy || !!draft.proxyUrl.trim()}
              onChange={(e) => setProxy({ server: e.target.value })}
            />
            <label className="set-label">{t("settings.proxyPort")}</label>
            <input
              className="mem-input set-narrow"
              placeholder="7890"
              value={draft.proxy.port ? String(draft.proxy.port) : ""}
              disabled={busy || !!draft.proxyUrl.trim()}
              inputMode="numeric"
              onChange={(e) => setProxy({ port: Number(e.target.value) || 0 })}
            />
            </div>
          </SettingsField>
          <SettingsField label={t("settings.proxyUsername")}>
            <div className="settings-inline-controls">
            <input
              className="mem-input set-grow"
              value={draft.proxy.username}
              disabled={busy || !!draft.proxyUrl.trim()}
              onChange={(e) => setProxy({ username: e.target.value })}
            />
            <label className="set-label">{t("settings.proxyPassword")}</label>
            <input
              className="mem-input set-grow"
              type="password"
              value={draft.proxy.password}
              disabled={busy || !!draft.proxyUrl.trim()}
              onChange={(e) => setProxy({ password: e.target.value })}
            />
            </div>
          </SettingsField>
          <SettingsField label={t("settings.proxyUrl")} hint={t("settings.proxyUrlHint")}>
              <input
                className="mem-input set-grow"
                placeholder="socks5://127.0.0.1:7890"
                value={draft.proxyUrl}
                disabled={busy}
                onChange={(e) => setDraft({ ...draft, proxyUrl: e.target.value })}
              />
          </SettingsField>
          <SettingsField label={t("settings.noProxy")}>
            <input
              className="mem-input set-grow"
              placeholder="localhost,127.0.0.1,.local"
              value={draft.noProxy}
              disabled={busy}
              onChange={(e) => setDraft({ ...draft, noProxy: e.target.value })}
            />
          </SettingsField>
        </>
      )}
    </SettingsSection>
  );
}

type BotInstallTarget = "qq" | "feishu" | "lark" | "weixin";
type HookScope = "global" | "project";

// HooksSection edits the hooks store (settings.json) for either scope (global or
// project), with a JSON editor (copy/paste/format) and the project-trust gate.
// Ported from DeepSeek-Reasonix, adapted to momapeer's onChanged signature.
function HooksSection({ onChanged }: { onChanged: () => void }) {
  const t = useT();
  const [scope, setScope] = useState<HookScope>("global");
  const [view, setView] = useState<HooksSettingsView | null>(null);
  const [jsonText, setJsonText] = useState("");
  const [jsonMessage, setJsonMessage] = useState<string | null>(null);
  const [jsonError, setJsonError] = useState<string | null>(null);
  const [pathMessage, setPathMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const load = useCallback(async (nextScope: HookScope) => {
    setBusy(true);
    setErr(null);
    try {
      const next = normalizeHooksSettingsView(await app.HooksSettings(nextScope), nextScope);
      setView(next);
      setJsonText(formatHooksJSON(next.hooks, next.events));
      setJsonMessage(null);
      setJsonError(null);
      setPathMessage(null);
    } catch (e) {
      setErr(String((e as Error)?.message ?? e));
      setView(null);
      setJsonText("");
      setJsonMessage(null);
      setJsonError(null);
      setPathMessage(null);
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    void load(scope);
  }, [load, scope]);

  const parseHooksEditorJSON = (raw = jsonText): { hooks: HookConfigView[]; text: string } | null => {
    try {
      const hooks = parseHooksJSON(raw, view?.events ?? []);
      const text = formatHooksJSON(hooks, view?.events ?? []);
      setJsonText(text);
      setJsonError(null);
      return { hooks, text };
    } catch (e) {
      setJsonError(t("settings.hooksJsonInvalid", { error: String((e as Error)?.message ?? e) }));
      setJsonMessage(null);
      return null;
    }
  };
  const copyHooksJSON = async () => {
    const parsed = parseHooksEditorJSON();
    if (!parsed) return;
    try {
      await navigator.clipboard?.writeText(parsed.text);
      setJsonMessage(t("settings.hooksJsonCopied"));
    } catch {
      setJsonMessage(t("settings.hooksJsonClipboardUnavailable"));
    }
  };
  const formatHooksEditorJSON = (raw = jsonText) => {
    const parsed = parseHooksEditorJSON(raw);
    if (parsed) setJsonMessage(t("settings.hooksJsonFormatted"));
  };
  const pasteHooksJSON = async () => {
    try {
      const raw = await navigator.clipboard?.readText();
      if (!raw) throw new Error(t("settings.hooksJsonClipboardEmpty"));
      setJsonText(raw);
      formatHooksEditorJSON(raw);
    } catch (e) {
      setJsonError(t("settings.hooksJsonPasteFailed", { error: String((e as Error)?.message ?? e) }));
      setJsonMessage(null);
    }
  };
  const copyHooksPath = async () => {
    const path = view?.path?.trim();
    if (!path) {
      setPathMessage(t("settings.hooksPathUnavailable"));
      return;
    }
    try {
      await navigator.clipboard?.writeText(path);
      setPathMessage(t("settings.hooksPathCopied"));
    } catch {
      setPathMessage(t("settings.hooksJsonClipboardUnavailable"));
    }
  };
  const save = async () => {
    setBusy(true);
    setErr(null);
    try {
      const parsed = parseHooksEditorJSON();
      if (!parsed) return;
      await app.SaveHooksSettingsForRoot(scope, view?.projectRoot?.trim() ?? "", parsed.hooks);
      await load(scope);
      onChanged();
    } catch (e) {
      setErr(String((e as Error)?.message ?? e));
    } finally {
      setBusy(false);
    }
  };
  const trustProject = async () => {
    const projectRoot = view?.projectRoot?.trim() ?? "";
    if (!projectRoot) {
      setErr(t("settings.hooksProjectRootUnavailable"));
      return;
    }
    setBusy(true);
    setErr(null);
    try {
      await app.TrustProjectHooksForRoot(projectRoot);
      await load("project");
      onChanged();
    } catch (e) {
      setErr(String((e as Error)?.message ?? e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      {err && <div className="banner banner--error">{err}</div>}
      <SettingsSection title={t("settings.hooksScopeSection")} description={t("settings.hooksScopeHint")}>
        <SettingsField label={t("settings.hooksScopeField")}>
          <select className="mem-select set-grow" value={scope} disabled={busy} onChange={(e) => setScope(e.target.value === "project" ? "project" : "global")}>
            <option value="global">{t("settings.hooksGlobal")}</option>
            <option value="project">{t("settings.hooksProject")}</option>
          </select>
        </SettingsField>
        <SettingsField label={t("settings.hooksPath")} hint={scope === "project" ? t("settings.hooksPathProjectHint") : t("settings.hooksPathGlobalHint")}>
          <div className="hooks-path-stack">
            <div className={`hooks-path-display${view?.path ? "" : " hooks-path-display--empty"}`}>
              <code className="hooks-path-display__value" title={view?.path || t("settings.hooksPathUnavailable")}>
                {view?.path || t("settings.hooksPathUnavailable")}
              </code>
              <button className="btn btn--small" disabled={busy || !view?.path} onClick={() => void copyHooksPath()}>{t("settings.hooksPathCopy")}</button>
            </div>
            {pathMessage && <div className="hooks-path-display__message">{pathMessage}</div>}
          </div>
        </SettingsField>
        {scope === "project" && (
          <SettingsField label={t("settings.hooksTrust")} hint={t("settings.hooksTrustHint")}>
            <div className="hooks-trust-stack">
              <div className="hooks-trust-row">
                <span className={`set-rule${view?.trusted ? "" : " set-rule--warn"}`}>{view?.trusted ? t("settings.hooksTrusted") : t("settings.hooksUntrusted")}</span>
                <button className="btn btn--small" disabled={busy || view?.trusted || !view?.projectRoot} onClick={() => void trustProject()}>{t("settings.hooksTrustProject")}</button>
              </div>
              <code className={`hooks-trust-root${view?.projectRoot ? "" : " hooks-trust-root--empty"}`} title={view?.projectRoot || t("settings.hooksProjectRootUnavailable")}>
                {view?.projectRoot || t("settings.hooksProjectRootUnavailable")}
              </code>
            </div>
          </SettingsField>
        )}
      </SettingsSection>

      <SettingsSection
        title={t("settings.hooks")}
        description={scope === "project" ? t("settings.hooksProjectHint") : t("settings.hooksGlobalHint")}
        actions={(
          <button className="btn btn--small btn--primary" disabled={busy} onClick={() => void save()}>{t("common.save")}</button>
        )}
      >
        {view && (
          <div className="hooks-json-panel">
            <div className="hooks-json-panel__head">
              <div>
                <div className="set-rules__label">{t("settings.hooksJsonTitle")}</div>
                <div className="set-rules__hint">{t("settings.hooksJsonHint")}</div>
              </div>
              <div className="hooks-json-panel__actions">
                <button className="btn btn--small" disabled={busy} onClick={() => void copyHooksJSON()}>{t("settings.hooksJsonCopy")}</button>
                <button className="btn btn--small" disabled={busy} onClick={() => void pasteHooksJSON()}>{t("settings.hooksJsonPaste")}</button>
                <button className="btn btn--small" disabled={busy || !jsonText.trim()} onClick={() => formatHooksEditorJSON()}>{t("settings.hooksJsonApply")}</button>
              </div>
            </div>
            <textarea
              className="mem-textarea hooks-json-panel__textarea"
              value={jsonText}
              disabled={busy}
              spellCheck={false}
              onChange={(e) => {
                setJsonText(e.target.value);
                setJsonMessage(null);
                setJsonError(null);
              }}
            />
            {jsonError && <div className="hooks-json-panel__message hooks-json-panel__message--error">{jsonError}</div>}
            {jsonMessage && <div className="hooks-json-panel__message">{jsonMessage}</div>}
          </div>
        )}
        {!view && <div className="empty">{t("settings.loading")}</div>}
      </SettingsSection>
    </>
  );
}

function normalizeHooksSettingsView(view: HooksSettingsView, scope: HookScope): HooksSettingsView {
  const events = asArray(view?.events).filter(Boolean);
  return {
    scope: view?.scope === "project" ? "project" : scope,
    path: view?.path ?? "",
    projectRoot: view?.projectRoot ?? "",
    trusted: !!view?.trusted,
    events,
    hooks: asArray(view?.hooks).map(normalizeHookConfig).filter((h) => h.event),
  };
}

function formatHooksJSON(hooks: HookConfigView[], eventOrder: string[]): string {
  const grouped: Record<string, Array<Record<string, string | number>>> = {};
  const events = new Set(eventOrder);
  for (const hook of hooks.map(normalizeHookConfig).filter((h) => h.event)) {
    events.add(hook.event);
    const entry: Record<string, string | number> = { command: hook.command };
    if (hook.match) entry.match = hook.match;
    if (hook.description) entry.description = hook.description;
    if ((hook.timeout ?? 0) > 0) entry.timeout = hook.timeout ?? 0;
    if (hook.cwd) entry.cwd = hook.cwd;
    (grouped[hook.event] ||= []).push(entry);
  }
  const ordered: typeof grouped = {};
  for (const event of [...eventOrder, ...Array.from(events).sort()]) {
    if (grouped[event]?.length && !ordered[event]) ordered[event] = grouped[event];
  }
  return JSON.stringify({ hooks: ordered }, null, 2);
}

function parseHooksJSON(raw: string, validEvents: string[]): HookConfigView[] {
  const trimmed = raw.trim();
  if (!trimmed) return [];
  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch (e) {
    throw new Error(String((e as Error)?.message ?? e));
  }
  if (Array.isArray(parsed)) {
    return parsed.map((item) => normalizeHookConfig(parseHookArrayItem(item, validEvents))).filter((h) => h.event);
  }
  if (!parsed || typeof parsed !== "object") {
    throw new Error("expected an object or array");
  }
  const obj = parsed as Record<string, unknown>;
  const hooksValue = obj.hooks && typeof obj.hooks === "object" && !Array.isArray(obj.hooks) ? obj.hooks : obj;
  return flattenHooksMap(hooksValue as Record<string, unknown>, validEvents);
}

function parseHookArrayItem(item: unknown, validEvents: string[]): HookConfigView {
  if (!item || typeof item !== "object" || Array.isArray(item)) throw new Error("hook item must be an object");
  const obj = item as Record<string, unknown>;
  const event = stringField(obj, "event") || "PreToolUse";
  if (validEvents.length > 0 && !validEvents.includes(event)) throw new Error(`unknown hook event ${event}`);
  return {
    event,
    match: stringField(obj, "match"),
    command: stringField(obj, "command"),
    description: stringField(obj, "description"),
    timeout: numberField(obj, "timeout"),
    cwd: stringField(obj, "cwd"),
  };
}

function flattenHooksMap(hooks: Record<string, unknown>, validEvents: string[]): HookConfigView[] {
  const valid = new Set(validEvents);
  const out: HookConfigView[] = [];
  for (const [event, value] of Object.entries(hooks)) {
    if (valid.size > 0 && !valid.has(event)) throw new Error(`unknown hook event ${event}`);
    const items = Array.isArray(value) ? value : [value];
    for (const item of items) {
      if (!item || typeof item !== "object" || Array.isArray(item)) throw new Error(`hook ${event} item must be an object`);
      const obj = item as Record<string, unknown>;
      out.push(normalizeHookConfig({
        event,
        match: stringField(obj, "match"),
        command: stringField(obj, "command"),
        description: stringField(obj, "description"),
        timeout: numberField(obj, "timeout"),
        cwd: stringField(obj, "cwd"),
      }));
    }
  }
  return out.filter((h) => h.event);
}

function stringField(obj: Record<string, unknown>, key: string): string {
  const value = obj[key];
  return typeof value === "string" ? value : "";
}

function numberField(obj: Record<string, unknown>, key: string): number {
  const value = obj[key];
  return typeof value === "number" && Number.isFinite(value) ? Math.floor(value) : 0;
}

function normalizeHookConfig(h: HookConfigView): HookConfigView {
  return {
    event: h.event || "PreToolUse",
    match: h.match ?? "",
    command: h.command ?? "",
    description: h.description ?? "",
    timeout: h.timeout && h.timeout > 0 ? Math.floor(h.timeout) : 0,
    cwd: h.cwd ?? "",
  };
}

type BotOfficialInstallTarget = Exclude<BotInstallTarget, "qq">;
type BotInstallState = {
  target: BotInstallTarget | "";
  result: BotInstallStartResult | null;
  status: "idle" | "starting" | "showing" | "connected" | "error";
  timeLeft: number;
  message: string;
};
const BOT_INSTALL_TARGETS: BotOfficialInstallTarget[] = ["feishu", "lark", "weixin"];
const BOT_INSTALL_DEFAULT_TIMEOUT_SECONDS = 300;
const BOT_INSTALL_MIN_POLL_SECONDS = 3;

function BotsSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  const savedBot = normalizeBotSettings(s.bot);
  const [draft, setDraft] = useState<BotSettingsView>(savedBot);
  const [installTarget, setInstallTarget] = useState<BotOfficialInstallTarget>("feishu");
  const [install, setInstall] = useState<BotInstallState>({ target: "feishu", result: null, status: "idle", timeLeft: 0, message: "" });
  const [diagnostics, setDiagnostics] = useState<Record<string, string>>({});
  const [testTargets, setTestTargets] = useState<Record<string, string>>({});
  const [connectionSecrets, setConnectionSecrets] = useState<Record<string, string>>({});
  const [expandedConnectionId, setExpandedConnectionId] = useState("");
  const installRef = useRef(install);
  const installPollTimerRef = useRef<number | null>(null);
  const installCountdownTimerRef = useRef<number | null>(null);
  const installRequestInFlightRef = useRef(false);
  const installAttemptRef = useRef(0);
  const refs = allRefs(s);

  useEffect(() => {
    setDraft(normalizeBotSettings(s.bot));
    setConnectionSecrets({});
    setTestTargets({});
  }, [s.bot]);
  useEffect(() => {
    installRef.current = install;
  }, [install]);
  useEffect(() => {
    installAttemptRef.current += 1;
    installRequestInFlightRef.current = false;
    clearInstallTimers();
    setInstall({ target: installTarget, result: null, status: "idle", timeLeft: 0, message: "" });
  }, [installTarget]);
  useEffect(() => () => {
    installAttemptRef.current += 1;
    clearInstallTimers();
  }, []);

  const dirty = JSON.stringify(sanitizeBotDraft(draft)) !== JSON.stringify(sanitizeBotDraft(savedBot));
  const setConnections = (mapper: (connections: BotConnectionView[]) => BotConnectionView[]) =>
    setDraft((prev) => ({ ...prev, connections: mapper(prev.connections) }));
  const updateConnection = (id: string, patch: Partial<BotConnectionView>) =>
    setConnections((items) => items.map((item) => item.id === id ? { ...item, ...patch } : item));
  const updateConnectionCredential = (id: string, patch: Partial<BotConnectionView["credential"]>) =>
    setConnections((items) => items.map((item) => item.id === id ? { ...item, credential: { ...item.credential, ...patch } } : item));
  const removeConnection = async (connection: BotConnectionView) => {
    const nextDraft = sanitizeBotDraft({
      ...draft,
      connections: draft.connections.filter((item) => item.id !== connection.id),
    });
    await apply(async () => {
      await app.SetBotSettings(nextDraft);
    });
  };
  const installQrURL = install.result?.url ?? "";
  const installQrIsImage = installQrURL.startsWith("data:image/");
  const selectedInstallConnection = draft.connections.find((connection) => botInstallTargetMatchesConnection(installTarget, connection));
  const selectedInstallLabel = botTargetLabel(installTarget, t);
  const installUserCode = install.result?.userCode && installTarget !== "weixin" ? formatInstallUserCode(install.result.userCode) : "";

  const saveBot = () => app.SetBotSettings(sanitizeBotDraft(draft));
  function clearInstallTimers() {
    if (installPollTimerRef.current !== null) {
      window.clearTimeout(installPollTimerRef.current);
      installPollTimerRef.current = null;
    }
    if (installCountdownTimerRef.current !== null) {
      window.clearInterval(installCountdownTimerRef.current);
      installCountdownTimerRef.current = null;
    }
  }
  function beginInstallCountdown(attempt: number) {
    if (installCountdownTimerRef.current !== null) {
      window.clearInterval(installCountdownTimerRef.current);
    }
    installCountdownTimerRef.current = window.setInterval(() => {
      setInstall((prev) => {
        if (installAttemptRef.current !== attempt || prev.status !== "showing") return prev;
        return { ...prev, timeLeft: Math.max(0, prev.timeLeft - 1) };
      });
    }, 1000);
  }
  function scheduleInstallPoll(attempt: number, interval: number) {
    if (installPollTimerRef.current !== null) {
      window.clearTimeout(installPollTimerRef.current);
    }
    installPollTimerRef.current = window.setTimeout(() => void pollInstall(attempt), Math.max(interval || BOT_INSTALL_MIN_POLL_SECONDS, BOT_INSTALL_MIN_POLL_SECONDS) * 1000);
  }
  const startInstall = async (target: BotOfficialInstallTarget = installTarget) => {
    if (installRequestInFlightRef.current) return;
    const existing = draft.connections.find((connection) => botInstallTargetMatchesConnection(target, connection));
    if (existing) {
      installAttemptRef.current += 1;
      clearInstallTimers();
      setInstall({ target, result: null, status: "connected", timeLeft: 0, message: t("settings.botInstallAlreadyConnected", { provider: botTargetLabel(target, t) }) });
      return;
    }
    clearInstallTimers();
    const attempt = installAttemptRef.current + 1;
    installAttemptRef.current = attempt;
    installRequestInFlightRef.current = true;
    setInstall({ target, result: null, status: "starting", timeLeft: 0, message: t("settings.botInstallStarting") });
    const provider = target === "weixin" ? "weixin" : "feishu";
    const domain = target === "lark" ? "lark" : target === "weixin" ? "weixin" : "feishu";
    try {
      const result = await app.StartBotConnectionInstall(provider, domain);
      if (installAttemptRef.current !== attempt) return;
      if (!result.ok) {
        setInstall({ target, result, status: "error", timeLeft: 0, message: result.message || t("settings.botInstallFailed") });
        return;
      }
      const timeLeft = result.expireIn > 0 ? result.expireIn : BOT_INSTALL_DEFAULT_TIMEOUT_SECONDS;
      setInstall({ target, result, status: "showing", timeLeft, message: result.message || t("settings.botInstallScanHint") });
      beginInstallCountdown(attempt);
      scheduleInstallPoll(attempt, result.interval);
    } catch (err) {
      if (installAttemptRef.current === attempt) {
        setInstall({ target, result: null, status: "error", timeLeft: 0, message: err instanceof Error ? err.message : t("settings.botInstallFailed") });
      }
    } finally {
      if (installAttemptRef.current === attempt) {
        installRequestInFlightRef.current = false;
      }
    }
  };
  const pollInstall = async (attempt = installAttemptRef.current) => {
    const current = installRef.current;
    if (installAttemptRef.current !== attempt || current.status !== "showing" || !current.result?.installId || !current.target) return;
    const poll = await app.PollBotConnectionInstall(current.result.installId);
    if (installAttemptRef.current !== attempt) return;
    if (poll.done) {
      clearInstallTimers();
      setDraft((prev) => ({
        ...prev,
        enabled: true,
        connections: [...prev.connections.filter((c) => c.id !== poll.connection.id), poll.connection],
      }));
      setInstall((prev) => ({ ...prev, status: "connected", timeLeft: 0, message: poll.message || t("settings.botInstallConnected") }));
      return;
    }
    if (poll.error) {
      clearInstallTimers();
      setInstall((prev) => ({ ...prev, status: "error", timeLeft: 0, message: poll.error }));
      return;
    }
    setInstall((prev) => ({ ...prev, message: poll.message || t("settings.botInstallWaiting") }));
    scheduleInstallPoll(attempt, current.result.interval);
  };
  useEffect(() => {
    if (install.status !== "showing" || install.timeLeft > 0) return;
    installAttemptRef.current += 1;
    clearInstallTimers();
    setInstall((prev) => prev.status === "showing" ? { ...prev, status: "error", message: t("settings.botInstallExpired") } : prev);
  }, [install.status, install.timeLeft]);
  const diagnoseConnection = async (id: string) => {
    const diag = await app.DiagnoseBotConnection(id);
    setDiagnostics((prev) => ({ ...prev, [id]: diag.message || diag.status }));
  };
  const testConnection = async (connection: BotConnectionView) => {
    const target = (testTargets[connection.id] ?? firstConnectionRemote(connection)).trim();
    const diag = await app.TestBotConnection(connection.id, target);
    setDiagnostics((prev) => ({ ...prev, [connection.id]: diag.message || diag.status }));
    if (diag.messageId && target) {
      const updatedAt = new Date().toISOString();
      setConnections((items) => items.map((item) => {
        if (item.id !== connection.id) return item;
        const scope = connection.workspaceRoot ? "project" : "global";
        const sessionMappings = [
          ...item.sessionMappings.filter((mapping) => mapping.remoteId !== target),
          { remoteId: target, sessionId: "", scope, workspaceRoot: scope === "project" ? connection.workspaceRoot : "", updatedAt },
        ];
        return { ...item, sessionMappings, updatedAt };
      }));
    }
  };
  const saveConnectionSecret = async (connection: BotConnectionView) => {
    const env = botConnectionSecretEnv(connection).trim();
    const value = (connectionSecrets[connection.id] ?? "").trim();
    if (!env || !value) return;
    await apply(async () => {
      await saveBot();
      await app.SetBotSecret(env, value);
    });
    setConnectionSecrets((prev) => ({ ...prev, [connection.id]: "" }));
  };
  const clearConnectionSecret = async (connection: BotConnectionView) => {
    const env = botConnectionSecretEnv(connection).trim();
    if (!env) return;
    await apply(async () => {
      await saveBot();
      await app.ClearBotSecret(env);
    });
  };

  return (
    <SettingsSection
      title={t("settings.botGateway")}
      description={t("settings.botGatewayHint")}
    >
      <div className="bot-phone-connect">
        <div className="bot-gateway-card">
          <div className="bot-gateway-card__copy">
            <strong>{t("settings.botGateway")}</strong>
            <span>{t("settings.botGatewayHint")}</span>
          </div>
          <div className="bot-gateway-card__actions">
            <div className="bot-phone-connect__switch">
              <span>{t("settings.botEnableBot")}</span>
              <ToggleSegment
                value={draft.enabled}
                disabled={busy}
                onChange={(enabled) => setDraft((prev) => ({ ...prev, enabled }))}
              />
            </div>
            <button
              className="btn btn--primary btn--small"
              disabled={busy || !dirty}
              onClick={() => void apply(saveBot)}
            >
              {t("settings.saveBotSettings")}
            </button>
          </div>
        </div>

        <div className="bot-access-mode">
          <strong>{t("settings.botAccessMode")}</strong>
          <div className="bot-access-mode__options">
            <label className="bot-access-mode__option">
              <input
                type="radio"
                name="bot-access-mode"
                checked={(draft.allowlist.mode || "open") === "open"}
                disabled={busy}
                onChange={() => setDraft((prev) => ({ ...prev, allowlist: { ...prev.allowlist, mode: "open" } }))}
              />
              <span>
                <strong>{t("settings.botAccessModeOpen")}</strong>
                <em>{t("settings.botAccessModeOpenHint")}</em>
              </span>
            </label>
            <label className="bot-access-mode__option">
              <input
                type="radio"
                name="bot-access-mode"
                checked={(draft.allowlist.mode || "open") === "review"}
                disabled={busy}
                onChange={() => setDraft((prev) => ({ ...prev, allowlist: { ...prev.allowlist, mode: "review" } }))}
              />
              <span>
                <strong>{t("settings.botAccessModeReview")}</strong>
                <em>{t("settings.botAccessModeReviewHint")}</em>
              </span>
            </label>
          </div>
        </div>

        <div className="bot-connection-list bot-connection-list--simple">
          <div className="bot-connection-list__head">
            <strong>{t("settings.botConnectedBots")}</strong>
          </div>
          {draft.connections.length === 0 ? (
            <div className="bot-connection-empty">{t("settings.botConnectionsEmpty")}</div>
          ) : (
            <div className="bot-connection-table" role="table" aria-label={t("settings.botConnectedBots")}>
              <div className="bot-connection-table__header" role="row">
                <span>{t("settings.botConnectionColumnChannel")}</span>
                <span>{t("settings.botConnectionColumnRemote")}</span>
                <span>{t("settings.botConnectionColumnScope")}</span>
                <span>{t("settings.botConnectionColumnStatus")}</span>
                <span>{t("settings.botConnectionColumnActions")}</span>
              </div>
              {draft.connections.map((connection) => (
                <div key={connection.id} className="bot-connection-row" role="rowgroup">
                  <div className="bot-connection-row__grid" role="row">
                    <div className="bot-connection-row__channel" role="cell">
                      <span>{connection.label || botConnectionLabel(connection, t)}</span>
                    </div>
                    <code role="cell">{botConnectionRemoteLabel(connection)}</code>
                    <span role="cell">{botConnectionScopeLabel(connection, t)}</span>
                    <span className={`bot-connection-row__status bot-connection-row__status--${connection.status === "connected" ? "connected" : "disconnected"}`} role="cell">
                      {connection.status === "connected" ? t("settings.botConnectionConnected") : connection.status || t("settings.botConnectionDisconnected")}
                    </span>
                    <div className="bot-connection-row__actions" role="cell">
                      <ToggleSegment
                        value={connection.enabled}
                        disabled={busy}
                        onChange={(enabled) => updateConnection(connection.id, { enabled })}
                      />
                      <button
                        type="button"
                        className="btn btn--secondary btn--small"
                        disabled={busy}
                        onClick={() => setExpandedConnectionId((current) => current === connection.id ? "" : connection.id)}
                      >
                        {t("settings.botManage")}
                      </button>
                    </div>
                  </div>
                  {diagnostics[connection.id] ? <em className="bot-connection-row__diag">{diagnostics[connection.id]}</em> : null}
                  {expandedConnectionId === connection.id ? (
                    <div className="bot-connection-manage">
                      <SettingsField label={t("settings.botConnectionActions")}>
                        <div className="bot-connection-manage__actions">
                          <button type="button" className="btn btn--secondary btn--small" disabled={busy} onClick={() => void diagnoseConnection(connection.id)}>
                            {t("settings.botDiagnose")}
                          </button>
                          {(connection.provider === "feishu" || connection.provider === "weixin") ? (
                            <button type="button" className="btn btn--secondary btn--small" disabled={busy} onClick={() => void testConnection(connection)}>
                              {t("settings.botTest")}
                            </button>
                          ) : null}
                        </div>
                      </SettingsField>
                      {(connection.provider === "feishu" || connection.provider === "weixin") ? (
                        <SettingsField label={t("settings.botTestChatId")}>
                          <input
                            className="mem-input"
                            value={testTargets[connection.id] ?? firstConnectionRemote(connection)}
                            disabled={busy}
                            placeholder={t("settings.botTestChatId")}
                            spellCheck={false}
                            onChange={(event) => setTestTargets((prev) => ({ ...prev, [connection.id]: event.target.value }))}
                          />
                        </SettingsField>
                      ) : null}
                      <SettingsField label={t("settings.botChannelModel")} hint={t("settings.botChannelModelHint")}>
                        <ModelPicker
                          s={s}
                          refs={refs}
                          value={toRef(connection.model, s)}
                          disabled={busy}
                          emptyOptionLabel={t("settings.botChannelModelAuto")}
                          emptyOptionHint={settingsModelMeta(s, t)}
                          onPick={(model) => updateConnection(connection.id, { model })}
                        />
                      </SettingsField>
                      <SettingsField label={t("settings.botWorkspaceRoot")} hint={t("settings.botWorkspaceRootHint")}>
                        <input
                          className="mem-input"
                          value={connection.workspaceRoot}
                          disabled={busy}
                          placeholder={t("settings.botWorkspaceRootPlaceholder")}
                          spellCheck={false}
                          onChange={(event) => updateConnection(connection.id, { workspaceRoot: event.target.value })}
                        />
                      </SettingsField>
                      <SettingsField label={t("settings.botCredential")}>
                        <div className="bot-credential-stack">
                          <div className="bot-credential-line">
                            <span>{botConnectionCredentialSummary(connection, t)}</span>
                            <strong>{connection.credential.secretSet ? t("settings.botSecretSet") : t("settings.botSecretMissing")}</strong>
                          </div>
                          {botConnectionSecretEnv(connection) ? (
                            <div className="bot-secret-row">
                              <input
                                className="mem-input"
                                value={botConnectionSecretEnv(connection)}
                                disabled={busy}
                                spellCheck={false}
                                onChange={(event) => updateConnectionCredential(connection.id, botConnectionSecretPatch(connection, event.target.value))}
                              />
                              <input
                                className="mem-input"
                                type="password"
                                value={connectionSecrets[connection.id] ?? ""}
                                disabled={busy}
                                placeholder={connection.credential.secretSet ? t("settings.botSecretReplace") : t("settings.botSecretPaste")}
                                onChange={(event) => setConnectionSecrets((prev) => ({ ...prev, [connection.id]: event.target.value }))}
                              />
                              <button type="button" className="btn btn--secondary btn--small" disabled={busy || !(connectionSecrets[connection.id] ?? "").trim()} onClick={() => void saveConnectionSecret(connection)}>
                                {t("settings.saveKey")}
                              </button>
                              <button type="button" className="btn btn--secondary btn--small" disabled={busy || !connection.credential.secretSet} onClick={() => void clearConnectionSecret(connection)}>
                                {t("settings.clearKey")}
                              </button>
                            </div>
                          ) : null}
                        </div>
                      </SettingsField>
                      <SettingsField label={t("settings.deleteBot")} hint={t("settings.deleteBotHint")}>
                        <div className="bot-connection-danger">
                          <InlineConfirmButton
                            label={t("settings.deleteBot")}
                            confirmLabel={t("settings.confirmDeleteBot")}
                            cancelLabel={t("common.cancel")}
                            disabled={busy}
                            danger
                            onConfirm={() => removeConnection(connection)}
                          />
                        </div>
                      </SettingsField>
                    </div>
                  ) : null}
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="bot-add-panel">
          <div className="bot-phone-connect__top">
            <div className="bot-phone-connect__title">
              <strong>{t("settings.botConnectPhoneTitle")}</strong>
              <span>{t("settings.botConnectPhoneSubtitle")}</span>
            </div>
          </div>

          <div className="bot-phone-targets" role="tablist" aria-label={t("settings.botChannels")}>
            {BOT_INSTALL_TARGETS.map((target) => (
              <button
                key={target}
                type="button"
                role="tab"
                aria-selected={installTarget === target}
                className={`bot-phone-target${installTarget === target ? " bot-phone-target--active" : ""}`}
                disabled={busy || install.status === "starting"}
                onClick={() => setInstallTarget(target)}
              >
                <strong>{botTargetLabel(target, t)}</strong>
                <span>{botTargetHint(target, t)}</span>
              </button>
            ))}
          </div>

          <div className="bot-connect-panel bot-connect-panel--phone">
            <div className="bot-connect-panel__qr">
              {selectedInstallConnection ? (
                <div className="bot-connect-panel__state bot-connect-panel__state--success">
                  <CheckCircle2 aria-hidden="true" />
                </div>
              ) : install.status === "showing" && installQrURL ? (
                installQrIsImage ? (
                  <img src={installQrURL} alt={t("settings.botInstallQrAlt")} />
                ) : (
                  <QRCodeSVG className="bot-connect-panel__qr-code" value={installQrURL} size={196} marginSize={1} />
                )
              ) : install.status === "starting" ? (
                <div className="bot-connect-panel__state">
                  <Loader2 className="bot-spin" aria-hidden="true" />
                  <span>{t("settings.botInstallStarting")}</span>
                </div>
              ) : install.status === "error" ? (
                <div className="bot-connect-panel__state bot-connect-panel__state--error">
                  <RefreshCw aria-hidden="true" />
                </div>
              ) : (
                <div className="bot-connect-panel__state">
                  <QrCode aria-hidden="true" />
                </div>
              )}
            </div>
            <div className="bot-connect-panel__body">
              <strong>{selectedInstallLabel}</strong>
              <p>
                {selectedInstallConnection
                  ? t("settings.botInstallAlreadyConnected", { provider: selectedInstallLabel })
                  : install.message || botTargetHint(installTarget, t)}
              </p>
              {install.status === "showing" && install.timeLeft > 0 ? (
                <span className="bot-connect-panel__timer">{t("settings.botInstallTimeLeft", { time: formatInstallTimeLeft(install.timeLeft) })}</span>
              ) : null}
              {installUserCode ? <code>{installUserCode}</code> : null}
              <div className="bot-connect-panel__actions">
                {!selectedInstallConnection && install.status !== "showing" && install.status !== "starting" ? (
                  <button type="button" className="btn btn--primary btn--small" disabled={busy} onClick={() => void startInstall()}>
                    {install.status === "error" ? <RefreshCw aria-hidden="true" /> : <QrCode aria-hidden="true" />}
                    {install.status === "error" ? t("settings.botInstallRetry") : t("settings.botInstallGenerate")}
                  </button>
                ) : null}
                {install.status === "showing" ? (
                  <button type="button" className="btn btn--secondary btn--small" disabled={busy} onClick={() => void pollInstall()}>
                    {t("settings.botInstallCheck")}
                  </button>
                ) : null}
                {selectedInstallConnection ? (
                  <button type="button" className="btn btn--secondary btn--small" disabled={busy} onClick={() => void diagnoseConnection(selectedInstallConnection.id)}>
                    {t("settings.botDiagnose")}
                  </button>
                ) : null}
              </div>
            </div>
          </div>
        </div>
      </div>
    </SettingsSection>
  );
}

function botTargetLabel(target: BotInstallTarget, t: ReturnType<typeof useT>): string {
  switch (target) {
    case "qq": return "QQ";
    case "lark": return "Lark";
    case "weixin": return t("settings.botWeixin");
    default: return t("settings.botFeishu");
  }
}

function botTargetHint(target: BotInstallTarget, t: ReturnType<typeof useT>): string {
  switch (target) {
    case "qq": return t("settings.botInstallQQHint");
    case "lark": return t("settings.botInstallLarkHint");
    case "weixin": return t("settings.botInstallWeixinHint");
    default: return t("settings.botInstallFeishuHint");
  }
}

function botInstallTargetMatchesConnection(target: BotOfficialInstallTarget, connection: BotConnectionView): boolean {
  if (target === "weixin") return connection.provider === "weixin";
  if (target === "lark") return connection.provider === "feishu" && connection.domain === "lark";
  return connection.provider === "feishu" && connection.domain !== "lark";
}

function formatInstallUserCode(code: string): string {
  const compact = code.replace(/[^a-z0-9]/gi, "").toUpperCase().slice(0, 8);
  if (compact.length <= 4) return compact;
  return `${compact.slice(0, 4)}-${compact.slice(4)}`;
}

function formatInstallTimeLeft(seconds: number): string {
  const value = Math.max(0, Math.floor(seconds));
  const minutes = Math.floor(value / 60);
  const rest = value % 60;
  return `${minutes}:${String(rest).padStart(2, "0")}`;
}

function botConnectionLabel(connection: BotConnectionView, t: ReturnType<typeof useT>): string {
  if (connection.domain === "lark") return "Lark";
  if (connection.provider === "weixin") return t("settings.botWeixin");
  if (connection.provider === "qq") return "QQ";
  return t("settings.botFeishu");
}

function firstConnectionRemote(connection: BotConnectionView): string {
  return connection.sessionMappings.find((mapping) => mapping.remoteId.trim())?.remoteId ?? "";
}

function botConnectionRemoteLabel(connection: BotConnectionView): string {
  return firstConnectionRemote(connection) || "—";
}

function botConnectionScopeLabel(connection: BotConnectionView, t: ReturnType<typeof useT>): string {
  return connection.workspaceRoot.trim() ? t("settings.botScopeProject") : t("settings.botScopeGlobal");
}

function botConnectionSecretEnv(connection: BotConnectionView): string {
  return connection.provider === "weixin" ? connection.credential.tokenEnv : connection.credential.appSecretEnv;
}

function botConnectionSecretPatch(connection: BotConnectionView, value: string): Partial<BotConnectionView["credential"]> {
  return connection.provider === "weixin" ? { tokenEnv: value } : { appSecretEnv: value };
}

function botConnectionCredentialSummary(connection: BotConnectionView, t: ReturnType<typeof useT>): string {
  if (connection.provider === "weixin") {
    return connection.credential.accountId
      ? t("settings.botCredentialAccount", { value: connection.credential.accountId })
      : t("settings.botCredentialLocalWeixin");
  }
  if (connection.credential.appId) {
    return t("settings.botCredentialApp", { value: connection.credential.appId });
  }
  return t("settings.botCredentialConfigured");
}

function ToggleSegment({
  value,
  disabled,
  onLabel,
  offLabel,
  onChange,
}: {
  value: boolean;
  disabled: boolean;
  onLabel?: string;
  offLabel?: string;
  onChange: (value: boolean) => void;
}) {
  const t = useT();
  return (
    <div className="set-seg">
      <button
        type="button"
        className={`set-seg__btn${value ? " set-seg__btn--on" : ""}`}
        disabled={disabled}
        onClick={() => onChange(true)}
      >
        {onLabel ?? t("settings.toggleOn")}
      </button>
      <button
        type="button"
        className={`set-seg__btn${!value ? " set-seg__btn--on" : ""}`}
        disabled={disabled}
        onClick={() => onChange(false)}
      >
        {offLabel ?? t("settings.toggleOff")}
      </button>
    </div>
  );
}

function sanitizeBotDraft(draft: BotSettingsView): BotSettingsView {
  const bot = normalizeBotSettings(draft);
  return {
    ...bot,
    model: bot.model.trim(),
    maxSteps: Math.max(0, Math.floor(bot.maxSteps || 0)),
    debounceMs: Math.max(0, Math.floor(bot.debounceMs || 0)),
    allowlist: {
      ...bot.allowlist,
      qqUsers: uniqueStrings(bot.allowlist.qqUsers.map((v) => v.trim())),
      feishuUsers: uniqueStrings(bot.allowlist.feishuUsers.map((v) => v.trim())),
      weixinUsers: uniqueStrings(bot.allowlist.weixinUsers.map((v) => v.trim())),
      qqGroups: uniqueStrings(bot.allowlist.qqGroups.map((v) => v.trim())),
      feishuGroups: uniqueStrings(bot.allowlist.feishuGroups.map((v) => v.trim())),
      weixinGroups: uniqueStrings(bot.allowlist.weixinGroups.map((v) => v.trim())),
    },
    qq: {
      ...bot.qq,
      appId: bot.qq.appId.trim(),
      appSecretEnv: bot.qq.appSecretEnv.trim(),
    },
    feishu: {
      ...bot.feishu,
      domain: bot.feishu.domain === "lark" ? "lark" : "feishu",
      appId: bot.feishu.appId.trim(),
      appSecretEnv: bot.feishu.appSecretEnv.trim(),
      verificationToken: bot.feishu.verificationToken.trim(),
      mode: bot.feishu.mode === "websocket" ? "websocket" : "webhook",
      webhookPort: Math.max(0, Math.floor(bot.feishu.webhookPort || 0)),
    },
    weixin: {
      ...bot.weixin,
      accountId: bot.weixin.accountId.trim(),
      tokenEnv: bot.weixin.tokenEnv.trim(),
      apiBase: bot.weixin.apiBase.trim().replace(/\/+$/, ""),
    },
    connections: bot.connections.map(normalizeBotConnection).filter((conn) => conn.id && conn.provider),
  };
}

function ModelsSection({ s, busy, apply, backgroundApply }: ModelsSectionProps) {
  const t = useT();
  const [subtab, setSubtab] = useState<"usage" | "access">("usage");
  const [jiutianDomain, setJiutianDomain] = useState("");
  useEffect(() => {
    app.GetJiutianBaseDomain().then(setJiutianDomain).catch(() => {});
  }, []);
  const autoRefreshKeyRef = useRef("");
  const refs = allRefs(s);
  const defaultRef = toRef(s.defaultModel, s);
  const fastTaskRef = toRef(s.fastTaskModel, s);
  const subagentRef = toRef(s.subagentModel, s);
  const fastTaskSelectRef = fastTaskRef === defaultRef ? "" : fastTaskRef;
  const [defaultProvider, defaultModel] = defaultRef.split("/");
  const defaultProviderView = s.providers.find((p) => p.name === defaultProvider);
  const currentModelLabel = defaultModel || defaultRef || t("common.none");
  const providerLabel = defaultProvider ? modelProviderLabel(defaultProvider, defaultProviderView, t) : t("common.none");
  const keyStatusLabel = defaultProviderView?.keySet ? t("settings.keySet") : t("settings.noKey");
  const agent = s.agent ?? { temperature: 0, maxSteps: 0, systemPrompt: "", rpm: 0 };
  const setAgentSteps = (maxSteps: number) => (
    app.SetAgentParams(agent.temperature, maxSteps, 0, agent.systemPrompt)
  );
  const setRPM = (rpm: number) => {
    // Clamp before sending to the backend: negative values disable (0), and
    // absurdly large values are capped to a sane single-key ceiling. 0 means
    // unlimited and is preserved. The backend accepts any int but a runaway
    // value would effectively disable limiting without any indication.
    const clamped = Number.isFinite(rpm) && rpm > 0 ? Math.min(Math.trunc(rpm), 600) : 0;
    return app.SetRPM(clamped);
  };

  useEffect(() => {
    if (subtab !== "usage") return;
    const groups = providerAccessGroups(s.providers.filter((p) => p.added), t);
    const candidates = groups
      .map((group) => {
        const provider = group.providers.find((p) => p.keySet && p.apiKeyEnv && p.baseUrl);
        return provider ? { group, provider } : null;
      })
      .filter((item): item is { group: ProviderAccessGroup; provider: ProviderView } => Boolean(item));
    const refreshKey = candidates.map(({ group, provider }) => `${group.id}:${provider.apiKeyEnv}`).join("|");
    if (!refreshKey || autoRefreshKeyRef.current === refreshKey) return;
    autoRefreshKeyRef.current = refreshKey;

    void backgroundApply(async () => {
      for (const { provider } of candidates) {
        // Background auto-refresh only protects a user-curated model list.
        // If the user hasn't specified any models, don't silently populate
        // the provider with every model from the API.
        if (!provider.models || provider.models.length === 0) continue;
        try {
          const fetched = await app.FetchProviderModels(provider);
          if (fetched.length === 0) continue;
          const models = mergedFetchedProviderModels(provider.models, fetched, { preserveCurated: true });
          const currentDefault = providerDefaultModel(provider.default, models);
          if (sameStringList(provider.models, models) && provider.default === currentDefault) continue;
          await app.SaveProvider({ ...provider, models, default: currentDefault });
        } catch {
          // Background discovery is opportunistic; manual refresh shows errors.
        }
      }
    });
  }, [backgroundApply, s.providers, subtab, t]);

  return (
    <>
      <div className="settings-subtabs">
        <button
          type="button"
          className={`settings-subtab${subtab === "usage" ? " settings-subtab--active" : ""}`}
          aria-selected={subtab === "usage"}
          onClick={() => setSubtab("usage")}
        >
          {t("settings.modelTab.usage")}
        </button>
        <button
          type="button"
          className={`settings-subtab${subtab === "access" ? " settings-subtab--active" : ""}`}
          aria-selected={subtab === "access"}
          onClick={() => setSubtab("access")}
        >
          {t("settings.modelTab.access")}
        </button>
      </div>

      {subtab === "usage" ? (
        <>
          <SettingsSection title={t("settings.modelUsage")}>
            <SettingsField label={t("settings.defaultModel")}>
              <ModelPicker
                s={s}
                refs={refs}
                value={toRef(s.defaultModel, s)}
                disabled={busy}
                onPick={(ref) => void apply(() => app.SetDefaultModel(ref))}
              />
            </SettingsField>

            <SettingsField label={"模型域名"}>
              <input
                className="mem-input"
                placeholder={t("settings.jiutianDomainHint")}
                value={jiutianDomain}
                onChange={e => setJiutianDomain(e.target.value)}
                onBlur={() => void app.SetJiutianBaseDomain(jiutianDomain)}
                onKeyDown={e => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); }}
              />
            </SettingsField>

            <SettingsField label={t("settings.screenshotVlmLabel")} hint={t("settings.screenshotVlmHint")}>
              <SimpleSelect
                value={s.cowork?.screenshotVlmModel || "qwen/qwen3.5-397b-a17b"}
                disabled={busy}
                options={[
                  { value: "qwen/qwen3.6-27b", label: `qwen/qwen3.6-27b — ${t("settings.screenshotVlmLight")}` },
                  { value: "qwen/qwen3.5-397b-a17b", label: `qwen/qwen3.5-397b-a17b — ${t("settings.screenshotVlmStrong")}` },
                ]}
                onChange={(vlm) => {
                  const base = s.cowork ?? {
                    browserPath: "", embeddingModel: "",
                    pptActiveTemplate: "", pptTemplates: [], pptTemplateDir: "",
                    smtpPassword: "", imapPassword: "", smtpPasswordSet: false, imapPasswordSet: false, detectedBrowser: "",
                    screenshotEnabled: false, screenshotHotkey: "Ctrl+Shift+S", screenshotVlmModel: "qwen/qwen3.6-27b",
                    estopHotkey: "Ctrl+Shift+Pause",
                  };
                  void apply(() => app.SetCoWorkSettings({ ...base, screenshotVlmModel: vlm } as any));
                }}
              />
            </SettingsField>

            <SettingsField label={t("settings.fastTaskModel")}>
              <ModelPicker
                s={s}
                refs={refs}
                value={fastTaskSelectRef}
                disabled={busy}
                includeSameDefault
                emptyOptionLabel={t("settings.fastTaskNone")}
                emptyOptionHint={t("settings.fastTaskNoneHintShort")}
                onPick={(ref) => void apply(() => app.SetFastTaskModel(ref))}
              />
            </SettingsField>

            <SettingsField label={t("settings.subagentModel")}>
              <ModelPicker
                s={s}
                refs={refs}
                value={subagentRef}
                disabled={busy}
                emptyOptionLabel={t("settings.subagentModelDefault")}
                emptyOptionHint={t("common.auto")}
                onPick={(ref) => void apply(() => app.SetSubagentModel(ref))}
              />
            </SettingsField>

            <SettingsField label={t("settings.subagentEffort")} hint={t("settings.subagentHint")}>
              <SimpleSelect
                value={s.subagentEffort || ""}
                disabled={busy}
                options={[
                  { value: "", label: t("settings.subagentEffortDefault") },
                  ...EFFORT_PRESETS.map((level) => ({ value: level, label: effortLabel(level, t) })),
                ]}
                onChange={(v) => void apply(() => app.SetSubagentEffort(v))}
              />
            </SettingsField>

            <div className="settings-model-current" aria-label={t("settings.modelCurrentStatus")}>
              <div>
                <span>{t("settings.modelCurrentStatus")}</span>
                <strong>{currentModelLabel}</strong>
              </div>
              <div className="settings-model-current__meta">
                <span>{providerLabel}</span>
                <span>{fastTaskSelectRef || t("settings.fastTaskNone")}</span>
                <span>{keyStatusLabel}</span>
              </div>
            </div>
          </SettingsSection>
          <SettingsSection title={t("settings.agentRuntime")} description={t("settings.agentRuntimeHint")}>
            <SettingsField label={t("settings.executorMaxSteps")} hint={t("settings.executorMaxStepsHint")}>
              <StepLimitControl
                value={agent.maxSteps}
                presets={[0, 10, 25, 50]}
                busy={busy}
                onChange={(next) => void apply(() => setAgentSteps(next))}
              />
            </SettingsField>
          </SettingsSection>
          <SettingsSection title={t("settings.rpm")} description={t("settings.rpmHint")}>
            <SettingsField label={t("settings.rpmLimit")} hint={t("settings.rpmLimitHint")}>
              <StepLimitControl
                value={agent.rpm}
                presets={[5, 20, 60, 180, 0]}
                busy={busy}
                onChange={(next) => void apply(() => setRPM(next))}
              />
            </SettingsField>
          </SettingsSection>
        </>
      ) : (
        <ProvidersSection s={s} busy={busy} apply={apply} />
      )}
    </>
  );
}

function WebSearchKeyField({
  label,
  apiKeyEnv,
  keySet,
  busy,
  onSave,
  onClear,
}: {
  label: string;
  apiKeyEnv: string;
  keySet?: boolean;
  busy: boolean;
  onSave: (env: string, value: string) => Promise<void>;
  onClear: (env: string) => Promise<void>;
}) {
  const t = useT();
  const [keyDraft, setKeyDraft] = useState("");

  const handleSave = () => {
    if (keyDraft.trim()) {
      onSave(apiKeyEnv, keyDraft.trim()).then(() => setKeyDraft(""));
    }
  };

  return (
    <SettingsField label={label} hint={apiKeyEnv}>
      <div className="set-key">
        <input
          type="password"
          className="mem-input set-grow"
          placeholder={keySet ? t("settings.keyIsSetPlaceholder") || "Key Set (Enter to update)" : t("settings.setKeyPlaceholder") || "Set API Key"}
          value={keyDraft}
          disabled={busy}
          onChange={(e) => setKeyDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && keyDraft.trim()) handleSave();
          }}
        />
        <button
          type="button"
          className="btn btn--small btn--primary"
          disabled={busy || !keyDraft.trim()}
          onClick={handleSave}
        >
          {t("common.save") || "Save"}
        </button>
        {keySet && (
          <button
            type="button"
            className="btn btn--small btn--danger"
            disabled={busy}
            onClick={() => onClear(apiKeyEnv)}
          >
            {t("settings.clearKey") || "Clear"}
          </button>
        )}
      </div>
    </SettingsField>
  );
}

type ModelPickerOption = {
  ref: string;
  provider: string;
  model: string;
  providerView?: ProviderView;
};

function ModelPicker({
  s,
  refs,
  value,
  disabled,
  includeSameDefault = false,
  emptyOptionLabel,
  emptyOptionHint,
  onPick,
}: {
  s: SettingsView;
  refs: string[];
  value: string;
  disabled: boolean;
  includeSameDefault?: boolean;
  emptyOptionLabel?: string;
  emptyOptionHint?: string;
  onPick: (ref: string) => void;
}) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const triggerRef = useRef<HTMLButtonElement>(null);
  const q = query.trim().toLowerCase();
  const emptyLabel = includeSameDefault ? t("settings.fastTaskNone") : emptyOptionLabel;
  const emptyHint = includeSameDefault ? t("settings.fastTaskNoneHint") : emptyOptionHint;
  const emptyMeta = includeSameDefault ? t("settings.fastTaskNoneHintShort") : emptyOptionHint;
  const selected = refs.includes(value) ? modelOptionFromRef(value, s) : null;
  const selectedLabel = value === "" && emptyLabel
    ? emptyLabel
    : selected?.model || value || t("common.none");
  const selectedMeta = value === "" && emptyLabel
    ? emptyMeta || ""
    : selected
    ? modelOptionMeta(selected, t)
    : t("settings.noModelsConfigured");
  const emptyOptionVisible = Boolean(emptyLabel) && (!q || `${emptyLabel} ${emptyHint || ""}`.toLowerCase().includes(q));

  const groups = useMemo(() => {
    const providerOrder: string[] = [];
    const providerSeen = new Set<string>();
    for (const p of s.providers) {
      const id = providerGroupID(p);
      if (!providerSeen.has(id)) {
        providerOrder.push(id);
        providerSeen.add(id);
      }
    }
    const options = refs
      .map((ref) => modelOptionFromRef(ref, s))
      .filter((opt): opt is ModelPickerOption => Boolean(opt))
      .filter((opt) => !q || `${opt.ref} ${opt.provider} ${modelProviderLabel(opt.provider, opt.providerView, t)} ${opt.model}`.toLowerCase().includes(q));
    for (const opt of options) {
      const groupID = modelOptionGroupID(opt);
      if (!providerSeen.has(groupID)) {
        providerOrder.push(groupID);
        providerSeen.add(groupID);
      }
    }
    return providerOrder
      .map((groupID) => {
        const providerViews = s.providers.filter((p) => providerGroupID(p) === groupID);
        const firstProvider = providerViews[0];
        return {
          groupID,
          label: firstProvider ? providerGroupLabel(firstProvider, t) : groupID,
          keySet: providerViews.some((p) => p.keySet),
          options: uniqueModelOptions(options.filter((opt) => modelOptionGroupID(opt) === groupID)),
        };
      })
      .filter((group) => group.options.length > 0);
  }, [q, refs, s, t]);

  useEffect(() => {
    if (!open) setQuery("");
  }, [open]);

  const pick = (ref: string) => {
    setOpen(false);
    if (ref !== value) onPick(ref);
  };

  return (
    <div className="settings-model-picker">
      <button
        ref={triggerRef}
        type="button"
        className="settings-model-picker__trigger"
        disabled={disabled || (!includeSameDefault && !emptyOptionLabel && refs.length === 0)}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((next) => !next)}
      >
        <span className="settings-model-picker__selected">
          <span>{selectedLabel}</span>
          <small>{selectedMeta}</small>
        </span>
        <ChevronDown size={16} className={`settings-model-picker__chev${open ? " settings-model-picker__chev--open" : ""}`} />
      </button>
      <AnchoredPopover
        open={open && !disabled}
        anchorRef={triggerRef}
        onClose={() => setOpen(false)}
        className="settings-model-picker__menu"
        placement="bottom"
        style={{ width: triggerRef.current?.getBoundingClientRect().width }}
      >
        <div className="settings-model-picker__search">
          <input
            value={query}
            placeholder={t("settings.searchModels")}
            onChange={(e) => setQuery(e.target.value)}
            autoFocus
          />
        </div>
        <div className="settings-model-picker__list" role="listbox">
          {emptyOptionVisible && (
            <button
              type="button"
              role="option"
              aria-selected={value === ""}
              className={`settings-model-picker__option settings-model-picker__option--pinned${value === "" ? " settings-model-picker__option--selected" : ""}`}
              onClick={() => pick("")}
            >
              <span>
                <strong>{emptyLabel}</strong>
                {emptyHint && <small>{emptyHint}</small>}
              </span>
              {value === "" && <Check size={14} />}
            </button>
          )}
          {groups.map((group) => (
            <div className="settings-model-picker__group" key={group.groupID}>
              <div className="settings-model-picker__group-title">
                <span>{group.label}</span>
                <small>{group.keySet ? t("settings.keySet") : t("settings.noKey")}</small>
              </div>
              {group.options.map((opt) => (
                <button
                  key={opt.ref}
                  type="button"
                  role="option"
                  aria-selected={opt.ref === value}
                  className={`settings-model-picker__option${opt.ref === value ? " settings-model-picker__option--selected" : ""}`}
                  onClick={() => pick(opt.ref)}
                >
                  <span>
                    <strong>{opt.model}</strong>
                    <small>{modelOptionMeta(opt, t)}</small>
                  </span>
                  {opt.ref === value && <Check size={14} />}
                </button>
              ))}
            </div>
          ))}
          {!emptyOptionVisible && groups.length === 0 && <div className="settings-model-picker__empty">{t("settings.noMatchingModels")}</div>}
        </div>
      </AnchoredPopover>
    </div>
  );
}

function modelOptionFromRef(ref: string, s: SettingsView): ModelPickerOption | null {
  if (!ref) return null;
  const [provider, ...modelParts] = ref.split("/");
  const model = modelParts.join("/") || ref;
  return {
    ref,
    provider,
    model,
    providerView: s.providers.find((p) => p.name === provider),
  };
}

function modelOptionMeta(option: ModelPickerOption, t: ReturnType<typeof useT>): string {
  const key = option.providerView?.keySet ? t("settings.keySet") : t("settings.noKey");
  return `${modelProviderLabel(option.provider, option.providerView, t)} · ${key}`;
}

function modelProviderLabel(provider: string, providerView: ProviderView | undefined, t: ReturnType<typeof useT>): string {
  return providerView ? providerGroupLabel(providerView, t) : provider;
}

function modelOptionGroupID(option: ModelPickerOption): string {
  return option.providerView ? providerGroupID(option.providerView) : `custom:${option.provider}`;
}

function uniqueModelOptions(options: ModelPickerOption[]): ModelPickerOption[] {
  const seen = new Set<string>();
  const out: ModelPickerOption[] = [];
  for (const option of options) {
    if (seen.has(option.model)) continue;
    seen.add(option.model);
    out.push(option);
  }
  return out;
}

// SimpleSelect is a styled dropdown that matches ModelPicker's visual appearance
// but without search or provider grouping. Used for fixed-option selects like
// VLM model and subagent effort.
function SimpleSelect({
  value,
  options,
  disabled,
  onChange,
}: {
  value: string;
  options: { value: string; label: string }[];
  disabled?: boolean;
  onChange: (value: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const selected = options.find((o) => o.value === value);

  return (
    <div className="settings-model-picker">
      <button
        ref={triggerRef}
        type="button"
        className="settings-model-picker__trigger"
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((next) => !next)}
      >
        <span className="settings-model-picker__selected">
          <span>{selected?.label || value}</span>
        </span>
        <ChevronDown size={16} className={`settings-model-picker__chev${open ? " settings-model-picker__chev--open" : ""}`} />
      </button>
      <AnchoredPopover
        open={open && !disabled}
        anchorRef={triggerRef}
        onClose={() => setOpen(false)}
        className="settings-model-picker__menu"
        placement="bottom"
        style={{ width: triggerRef.current?.getBoundingClientRect().width }}
      >
        <div className="settings-model-picker__list" role="listbox">
          {options.map((opt) => (
            <button
              key={opt.value}
              type="button"
              role="option"
              aria-selected={opt.value === value}
              className={`settings-model-picker__option${opt.value === value ? " settings-model-picker__option--selected" : ""}`}
              onClick={() => { setOpen(false); if (opt.value !== value) onChange(opt.value); }}
            >
              <span><strong>{opt.label}</strong></span>
              {opt.value === value && <Check size={14} />}
            </button>
          ))}
        </div>
      </AnchoredPopover>
    </div>
  );
}

function sameStringList(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  return a.every((value, i) => value === b[i]);
}

function proxyModeLabel(mode: ProxyMode, t: ReturnType<typeof useT>): string {
  switch (mode) {
    case "auto":
      return t("settings.proxyMode.auto");
    case "custom":
      return t("settings.proxyMode.custom");
    case "off":
      return t("settings.proxyMode.off");
  }
}

// 工具名到状态键的映射
const JIUTIAN_TOOL_KEYS: Record<string, keyof { imageUnderstand: boolean; imageGenerate: boolean; videoUnderstand: boolean }> = {
  image_understand: "imageUnderstand",
  image_generate: "imageGenerate",
  video_understand: "videoUnderstand",
};

function JiutianSection() {
  const t = useT();
  const [jiutian, setJiutian] = useState<{ imageUnderstand: boolean; imageGenerate: boolean; videoUnderstand: boolean } | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    app.Settings()
      .then((s) => setJiutian(s.jiutian ?? { imageUnderstand: true, imageGenerate: false, videoUnderstand: false }))
      .catch(() => {});
  }, []);

  const toggle = async (name: string, value: boolean) => {
    if (busy) return;
    // 乐观更新：点击立即反映到 UI，不等待异步完成。成功后保持乐观值，不再
    // 立即回读 app.Settings()——后端写读现已同源(applyConfigChange)，回读
    // 只会把刚写入的值原样覆盖回来，徒增把开关"弹回"的风险。
    const stateKey = JIUTIAN_TOOL_KEYS[name];
    const prev = jiutian;
    if (stateKey) setJiutian((p) => p ? { ...p, [stateKey]: value } : p);
    setBusy(true);
    try {
      await app.SetJiutianTool(name, value);
    } catch {
      // 写入失败：回滚到点击前的本地状态，而非再调 app.Settings()。
      setJiutian(prev);
    } finally {
      setBusy(false);
    }
  };

  if (!jiutian) return null;

  const fields: Array<{ key: keyof typeof jiutian; labelKey: DictKey; hintKey: DictKey; toolName: string }> = [
    { key: "imageGenerate",   labelKey: "settings.jiutianImageGenerate",   hintKey: "settings.jiutianImageGenerateHint",   toolName: "image_generate" },
    { key: "videoUnderstand", labelKey: "settings.jiutianVideoUnderstand", hintKey: "settings.jiutianVideoUnderstandHint", toolName: "video_understand" },
  ];

  return (
    <section className="mem-section cap-tools">
      <div className="cap-tools__head">
        <div className="cap-tools__title">{t("settings.jiutianTitle")}</div>
        <div className="cap-tools__summary">{t("settings.jiutianSummary")}</div>
      </div>
      <div className="cap-tools__list">
        {fields.map(({ key, labelKey, hintKey, toolName }) => {
          const label = t(labelKey);
          const hint = t(hintKey);
          return (
            <div key={key} className={`cap-tool-row${!jiutian[key] ? " cap-tool-row--off" : ""}`}>
              <div className="cap-tool-row__copy">
                <div className="cap-tool-row__label">{label}</div>
                <div className="cap-tool-row__hint">{hint}</div>
              </div>
              <Tooltip label={`${label}${jiutian[key] ? t("settings.jiutianDisable") : t("settings.jiutianEnable")}`}>
                <label className="cap-switch" aria-label={`${label}${jiutian[key] ? t("settings.jiutianDisable") : t("settings.jiutianEnable")}`}>
                  <input
                    type="checkbox"
                    checked={jiutian[key]}
                    disabled={busy}
                    onChange={(e) => void toggle(toolName, e.target.checked)}
                  />
                  <span className="cap-switch__track" />
                </label>
              </Tooltip>
            </div>
          );
        })}
      </div>
    </section>
  );
}

function WebSearchSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  return (
    <SettingsSection title={t("settings.webSearchTitle") || "Web Search Engines (Fallback Chain)"} description={t("settings.webSearchDesc") || "Configure API keys for the native web_search tool. The system iterates through these engines securely."}>
      <WebSearchKeyField
        label="Brave Search"
        apiKeyEnv="BRAVE_API_KEY"
        keySet={s.webSearch?.braveKeySet}
        busy={busy}
        onSave={(env, val) => apply(() => app.SetProviderKey(env, val))}
        onClear={(env) => apply(() => app.ClearProviderKey(env))}
      />
      <WebSearchKeyField
        label="Exa Search"
        apiKeyEnv="EXA_API_KEY"
        keySet={s.webSearch?.exaKeySet}
        busy={busy}
        onSave={(env, val) => apply(() => app.SetProviderKey(env, val))}
        onClear={(env) => apply(() => app.ClearProviderKey(env))}
      />
      <WebSearchKeyField
        label="Linkup Search"
        apiKeyEnv="LINKUP_API_KEY"
        keySet={s.webSearch?.linkupKeySet}
        busy={busy}
        onSave={(env, val) => apply(() => app.SetProviderKey(env, val))}
        onClear={(env) => apply(() => app.ClearProviderKey(env))}
      />
    </SettingsSection>
  );
}

function ProvidersSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  const defaultProvider = toRef(s.defaultModel, s).split("/")[0];
  const [editing, setEditing] = useState<string | null>(null);
  const [adding, setAdding] = useState<AddProviderMode>(null);
  const [fetchingProvider, setFetchingProvider] = useState<string | null>(null);
  const [fetchResults, setFetchResults] = useState<Record<string, ProviderFetchResult>>({});
  const [modelDrafts, setModelDrafts] = useState<Record<string, ProviderModelDraft>>({});
  const groups = providerAccessGroups(s.providers.filter((p) => p.added), t);

  const setGroupFetchResult = (groupID: string, result: ProviderFetchResult | null) => {
    setFetchResults((prev) => {
      const next = { ...prev };
      if (result) next[groupID] = result;
      else delete next[groupID];
      return next;
    });
  };

  const setGroupModelDraft = (groupID: string, draft: ProviderModelDraft | null) => {
    setModelDrafts((prev) => {
      const next = { ...prev };
      if (draft) next[groupID] = draft;
      else delete next[groupID];
      return next;
    });
  };

  const modelDraftForFetch = (p: ProviderView, fetched: string[]): ProviderModelDraft => {
    const candidates = providerModelCandidates(p.models, fetched);
    const selected = mergedFetchedProviderModels(p.models, fetched, { preserveCurated: true });
    return {
      providerName: p.name,
      candidates,
      selected: candidates.filter((model) => selected.includes(model)),
    };
  };

  const updateModelDraftSelection = (groupID: string, nextSelected: (draft: ProviderModelDraft) => string[]) => {
    setModelDrafts((prev) => {
      const draft = prev[groupID];
      if (!draft) return prev;
      const selectedSet = new Set(nextSelected(draft));
      return {
        ...prev,
        [groupID]: {
          ...draft,
          selected: draft.candidates.filter((model) => selectedSet.has(model)),
        },
      };
    });
  };

  const refreshModels = async (group: ProviderAccessGroup, p: ProviderView) => {
    setFetchingProvider(group.id);
    setGroupFetchResult(group.id, null);
    setGroupModelDraft(group.id, null);
    try {
      let fetched: string[];
      try {
        fetched = await app.FetchProviderModels(p);
      } catch (e) {
        setGroupFetchResult(group.id, {
          kind: "warn",
          text: t("settings.fetchModelsFailedForProvider", { provider: group.label, err: String((e as Error)?.message ?? e) }),
        });
        return;
      }
      if (fetched.length === 0) {
        setGroupFetchResult(group.id, {
          kind: "warn",
          text: t("settings.fetchModelsEmptyForProvider", { provider: group.label }),
        });
        return;
      }
      const draft = modelDraftForFetch(p, fetched);
      setGroupModelDraft(group.id, draft);
      setGroupFetchResult(group.id, {
        kind: "ok",
        text: t("settings.fetchModelsReadyForProvider", { provider: group.label, n: draft.candidates.length }),
      });
    } finally {
      setFetchingProvider(null);
    }
  };

  const refreshGroup = async (group: ProviderAccessGroup) => {
    const probe = group.providers[0];
    if (!probe) return;
    await refreshModels(group, probe);
  };

  const saveKeyEnvAndAutoRefresh = async (group: ProviderAccessGroup, apiKeyEnv: string, value: string) => {
    const probe = group.providers[0];
    if (!probe || !apiKeyEnv) return;
    setFetchingProvider(group.id);
    setGroupFetchResult(group.id, null);
    setGroupModelDraft(group.id, null);
    try {
      await apply(async () => {
        await app.SetProviderKey(apiKeyEnv, value);
        try {
          const fetched = await app.FetchProviderModels({ ...probe, apiKeyEnv });
          if (fetched.length > 0) {
            const draft = modelDraftForFetch({ ...probe, apiKeyEnv }, fetched);
            setGroupModelDraft(group.id, draft);
            setGroupFetchResult(group.id, {
              kind: "ok",
              text: t("settings.fetchModelsReadyForProvider", { provider: group.label, n: draft.candidates.length }),
            });
            return;
          }
          setGroupFetchResult(group.id, {
            kind: "warn",
            text: t("settings.fetchModelsEmptyForProvider", { provider: group.label }),
          });
        } catch (e) {
          setGroupFetchResult(group.id, {
            kind: "warn",
            text: t("settings.fetchModelsAfterKeyFailedForProvider", { provider: group.label, err: String((e as Error)?.message ?? e) }),
          });
        }
      });
    } finally {
      setFetchingProvider(null);
    }
  };

  const saveProviderKey = async (group: ProviderAccessGroup, apiKeyEnv: string, value: string) => {
    if (!apiKeyEnv) return;
    setGroupFetchResult(group.id, null);
    setGroupModelDraft(group.id, null);
    await apply(() => app.SetProviderKey(apiKeyEnv, value));
  };

  const clearProviderKey = async (apiKeyEnv: string) => {
    if (!apiKeyEnv) return;
    await apply(() => app.ClearProviderKey(apiKeyEnv));
  };

  const saveModelDraft = async (group: ProviderAccessGroup) => {
    const draft = modelDrafts[group.id];
    const provider = draft ? group.providers.find((p) => p.name === draft.providerName) : null;
    const models = uniqueStrings(draft?.selected ?? []);
    if (!draft || !provider || models.length === 0) return;
    let saved = false;
    await apply(async () => {
      await app.SaveProvider({ ...provider, models, default: providerDefaultModel(provider.default, models) });
      saved = true;
    });
    if (!saved) return;
    setGroupModelDraft(group.id, null);
    setGroupFetchResult(group.id, {
      kind: "ok",
      text: t("settings.enabledModelsSavedForProvider", { provider: group.label, n: models.length }),
    });
  };

  return (
    <SettingsSection
      title={t("settings.providerAccess")}
      description={t("settings.providerAccessHint")}
      actions={
        <button className="btn btn--small" disabled={busy || adding !== null} onClick={() => setAdding("official")}>
          {t("settings.addProvider")}
        </button>
      }
    >
      <div className="provider-access-grid">
        {groups.length === 0 && adding === null && (
          <div className="provider-empty">
            <strong>{t("settings.providerAccessEmptyTitle")}</strong>
            <span>{t("settings.providerAccessEmptyHint")}</span>
            <div className="provider-empty__actions">
              <button type="button" className="btn btn--small" disabled={busy} onClick={() => setAdding("official")}>
                {t("settings.addProvider.officialChoice")}
              </button>
              <button type="button" className="btn btn--small" disabled={busy} onClick={() => setAdding("custom")}>
                {t("settings.addProvider.customChoice")}
              </button>
            </div>
          </div>
        )}
        {adding !== null && (
          <AddProviderPanel
            mode={adding}
            kinds={s.providerKinds}
            busy={busy}
            onMode={setAdding}
            onCancel={() => setAdding(null)}
            onAddOfficial={(kind, key) => apply(() => app.AddOfficialProviderAccess(kind, key)).then(() => setAdding(null))}
            onAddCustom={(pv) => apply(() => app.SaveProvider(pv)).then(() => setAdding(null))}
          />
        )}
        {adding === null && groups.map((group) => (
          <ProviderAccessCard
            key={group.id}
            group={group}
            busy={busy}
            fetching={fetchingProvider === group.id || group.providers.some((p) => fetchingProvider === p.name)}
            fetchResult={fetchResults[group.id]}
            modelDraft={modelDrafts[group.id]}
            defaultProvider={defaultProvider}
            editing={editing}
            kinds={s.providerKinds}
            onEdit={setEditing}
            onCancelEdit={() => setEditing(null)}
            onSave={(pv) => apply(() => app.SaveProvider(pv)).then(() => {
              setEditing(null);
              setGroupModelDraft(group.id, null);
            })}
            onRefresh={() => void refreshGroup(group)}
            onToggleDraftModel={(model) => updateModelDraftSelection(group.id, (draft) => (
              draft.selected.includes(model)
                ? draft.selected.filter((candidate) => candidate !== model)
                : [...draft.selected, model]
            ))}
            onSelectAllDraftModels={() => updateModelDraftSelection(group.id, (draft) => draft.candidates)}
            onClearDraftModels={() => updateModelDraftSelection(group.id, () => [])}
            onCancelDraftModels={() => setGroupModelDraft(group.id, null)}
            onSaveDraftModels={() => void saveModelDraft(group)}
            onSaveEditorKey={(env, value) => group.builtIn ? saveProviderKey(group, env, value) : saveKeyEnvAndAutoRefresh(group, env, value)}
            onClearEditorKey={clearProviderKey}
            onDelete={(p) => apply(() => app.RemoveProviderAccess(p.name))}
          />
        ))}
      </div>
    </SettingsSection>
  );
}

type ProviderAccessGroup = {
  id: string;
  label: string;
  description: string;
  builtIn: boolean;
  providers: ProviderView[];
  apiKeyEnv: string;
  keySet: boolean;
  baseUrl: string;
  kind: string;
  models: string[];
};

type ProviderFetchResult = {
  kind: "ok" | "warn";
  text: string;
};

type ProviderModelDraft = {
  providerName: string;
  candidates: string[];
  selected: string[];
};

type AddProviderMode = null | "official" | "custom";
type OfficialProviderKind = "moma";

const OFFICIAL_PROVIDER_CHOICES: Array<{ kind: OfficialProviderKind; labelKey: DictKey; descKey: DictKey; keyEnv: string }> = [
  { kind: "moma", labelKey: "settings.addProvider.official.moma", descKey: "settings.addProvider.official.momaDesc", keyEnv: "JIUTIAN_API_KEY" },
];

function AddProviderPanel({
  mode,
  kinds,
  busy,
  onMode,
  onCancel,
  onAddOfficial,
  onAddCustom,
}: {
  mode: AddProviderMode;
  kinds: string[];
  busy: boolean;
  onMode: (mode: AddProviderMode) => void;
  onCancel: () => void;
  onAddOfficial: (kind: OfficialProviderKind, key: string) => Promise<void>;
  onAddCustom: (p: ProviderView) => void | Promise<void>;
}) {
  const t = useT();
  const [officialKind, setOfficialKind] = useState<OfficialProviderKind>("moma");
  const [key, setKey] = useState("");
  const selected = OFFICIAL_PROVIDER_CHOICES.find((choice) => choice.kind === officialKind) ?? OFFICIAL_PROVIDER_CHOICES[0];

  const header = (
    <div className="provider-add-panel__head">
      <div>
        <strong>{t("settings.addProvider.chooseTitle")}</strong>
        <span>{t("settings.addProvider.chooseHint")}</span>
      </div>
      <button type="button" className="btn btn--small" disabled={busy} onClick={onCancel}>
        {t("common.cancel")}
      </button>
    </div>
  );
  const modeSwitch = (
    <div className="provider-add-segmented" role="tablist" aria-label={t("settings.addProvider.chooseTitle")}>
      <button
        type="button"
        role="tab"
        aria-selected={mode === "official"}
        className={mode === "official" ? "provider-add-segmented__item provider-add-segmented__item--active" : "provider-add-segmented__item"}
        disabled={busy}
        onClick={() => onMode("official")}
      >
        {t("settings.addProvider.officialChoice")}
      </button>
      <button
        type="button"
        role="tab"
        aria-selected={mode === "custom"}
        className={mode === "custom" ? "provider-add-segmented__item provider-add-segmented__item--active" : "provider-add-segmented__item"}
        disabled={busy}
        onClick={() => onMode("custom")}
      >
        {t("settings.addProvider.customChoice")}
      </button>
    </div>
  );

  if (mode === "official") {
    return (
      <div className="provider-add-panel">
        {header}
        {modeSwitch}
        <div className="provider-add-panel__hint">{t("settings.addProvider.officialHint")}</div>
        <div className="provider-template-grid">
          {OFFICIAL_PROVIDER_CHOICES.map((choice) => (
            <button
              key={choice.kind}
              type="button"
              className={`provider-template-card${officialKind === choice.kind ? " provider-template-card--active" : ""}`}
              disabled={busy}
              onClick={() => setOfficialKind(choice.kind)}
            >
              <strong>{t(choice.labelKey)}</strong>
              <span>{t(choice.descKey)}</span>
            </button>
          ))}
        </div>
        <label className="set-label">{t("settings.providerKeyOptional")}</label>
        <input
          className="mem-input"
          type="password"
          placeholder={t("settings.setKey", { env: selected.keyEnv })}
          value={key}
          disabled={busy}
          onChange={(e) => setKey(e.target.value)}
        />
        <div className="prov-card__actions">
          <button type="button" className="btn btn--small" disabled={busy} onClick={onCancel}>
            {t("common.cancel")}
          </button>
          <button
            type="button"
            className="btn btn--primary btn--small"
            disabled={busy}
            onClick={() => void onAddOfficial(officialKind, key.trim())}
          >
            {t("settings.addProvider.confirm")}
          </button>
        </div>
      </div>
    );
  }

  if (mode === "custom") {
    return (
      <div className="provider-add-panel">
        {header}
        {modeSwitch}
        <div className="provider-add-panel__hint">{t("settings.addProvider.customHint")}</div>
        <ProviderEditor
          kinds={kinds}
          busy={busy}
          onCancel={onCancel}
          onSave={onAddCustom}
        />
      </div>
    );
  }
  return null;
}

function ProviderAccessCard({
  group,
  busy,
  fetching,
  fetchResult,
  modelDraft,
  defaultProvider,
  editing,
  kinds,
  onEdit,
  onCancelEdit,
  onSave,
  onRefresh,
  onToggleDraftModel,
  onSelectAllDraftModels,
  onClearDraftModels,
  onCancelDraftModels,
  onSaveDraftModels,
  onSaveEditorKey,
  onClearEditorKey,
  onDelete,
}: {
  group: ProviderAccessGroup;
  busy: boolean;
  fetching: boolean;
  fetchResult?: ProviderFetchResult;
  modelDraft?: ProviderModelDraft;
  defaultProvider: string;
  editing: string | null;
  kinds: string[];
  onEdit: (name: string) => void;
  onCancelEdit: () => void;
  onSave: (p: ProviderView) => void | Promise<void>;
  onRefresh: () => void;
  onToggleDraftModel: (model: string) => void;
  onSelectAllDraftModels: () => void;
  onClearDraftModels: () => void;
  onCancelDraftModels: () => void;
  onSaveDraftModels: () => void;
  onSaveEditorKey: (apiKeyEnv: string, value: string) => Promise<void>;
  onClearEditorKey?: (apiKeyEnv: string) => Promise<void>;
  onDelete?: (p: ProviderView) => Promise<void>;
}) {
  const t = useT();
  const editableProvider = group.providers[0];
  const isDefault = group.providers.some((p) => p.name === defaultProvider);
  const editingProvider = group.providers.find((p) => editing === p.name);
  const primaryProviderExpanded = Boolean(editableProvider && editing === editableProvider.name);
  const visibleModels = group.models.slice(0, 6);
  const hiddenModelCount = Math.max(0, group.models.length - visibleModels.length);
  return (
    <article className={`provider-access-card${group.builtIn ? " provider-access-card--builtin" : ""}`}>
      <div className="provider-access-card__head">
        <div className="provider-access-card__identity">
          <div className="provider-access-card__title">
            {group.label}
            <span className={`badge ${group.builtIn ? "badge--project" : "badge--neutral"}`}>
              {group.builtIn ? t("settings.builtinProviderBadge") : t("settings.customProviderBadge")}
            </span>
            <span className={`badge ${group.keySet ? "badge--project" : "badge--feedback"}`}>
              {group.keySet ? t("settings.keySet") : t("settings.noKey")}
            </span>
          </div>
          <div className="provider-access-card__desc">{group.description}</div>
        </div>
        <div className="provider-access-card__actions">
          {editableProvider && (
            <button
              className="btn btn--small"
              disabled={busy}
              aria-expanded={primaryProviderExpanded}
              onClick={() => primaryProviderExpanded ? onCancelEdit() : onEdit(editableProvider.name)}
            >
              {primaryProviderExpanded ? t("common.collapse") : t("settings.configureProvider")}
            </button>
          )}
          <button
            className="btn btn--small"
            disabled={busy || fetching || !group.baseUrl || !group.apiKeyEnv || !group.keySet}
            onClick={onRefresh}
          >
            {fetching ? t("settings.fetchingModels") : t("settings.fetchModels")}
          </button>
          {editableProvider && onDelete && (
            isDefault && !group.builtIn ? (
              <Tooltip label={t("settings.cantDeleteDefault")}>
                <button className="btn btn--small" disabled>{t("settings.removeProviderAccess")}</button>
              </Tooltip>
            ) : (
              <InlineConfirmButton
                label={t("settings.removeProviderAccess")}
                confirmLabel={group.builtIn ? t("settings.confirmRemoveProviderAccess") : t("settings.confirmDeleteProvider")}
                cancelLabel={t("common.cancel")}
                disabled={busy}
                danger={!group.builtIn}
                onConfirm={() => onDelete(editableProvider)}
              />
            )
          )}
        </div>
      </div>

      <div className="provider-access-meta">
        <span>{group.kind}</span>
        <span>{group.baseUrl}</span>
        <span>{group.apiKeyEnv || t("common.none")}</span>
      </div>

      <div className="provider-card-block">
        <div className="provider-card-block__label">{t(group.keySet ? "settings.enabledModels" : "settings.modelList")}</div>
        <div className="provider-model-chips" aria-label={t(group.keySet ? "settings.enabledModels" : "settings.modelList")}>
          {visibleModels.length > 0 ? visibleModels.map((model) => (
            <span className="provider-model-chip" key={model}>
              {model}
            </span>
          )) : <span className="provider-model-chip provider-model-chip--empty">{t("settings.noModelsConfigured")}</span>}
          {hiddenModelCount > 0 && (
            <span className="provider-model-chip provider-model-chip--more">
              {t("settings.moreModels", { n: hiddenModelCount })}
            </span>
          )}
        </div>
        {!group.keySet && (
          <div className="provider-card-status provider-card-status--warn">
            {t("settings.modelsRequireKey")}
          </div>
        )}
        {fetchResult && (
          <div className={`provider-card-status provider-card-status--${fetchResult.kind}`}>
            {fetchResult.text}
          </div>
        )}
      </div>

      {modelDraft && (
        <ProviderModelDraftPicker
          draft={modelDraft}
          busy={busy}
          fetching={fetching}
          onToggle={onToggleDraftModel}
          onSelectAll={onSelectAllDraftModels}
          onClear={onClearDraftModels}
          onCancel={onCancelDraftModels}
          onSave={onSaveDraftModels}
        />
      )}

      {group.providers.length > 1 && (
        <div className="provider-profiles">
          {group.providers.map((p) => {
            const profileExpanded = editing === p.name;
            return (
              <div className="provider-profile-row" key={p.name}>
                <span>{p.name}</span>
                <span>{p.models.join(", ") || t("common.none")}</span>
                <button
                  className="btn btn--small"
                  disabled={busy}
                  aria-expanded={profileExpanded}
                  onClick={() => profileExpanded ? onCancelEdit() : onEdit(p.name)}
                >
                  {profileExpanded ? t("common.collapse") : t("settings.configureProfile")}
                </button>
              </div>
            );
          })}
        </div>
      )}

      {editingProvider && (
        <ProviderEditor
          initial={editingProvider}
          kinds={kinds}
          busy={busy}
          onCancel={onCancelEdit}
          onSave={onSave}
          onSaveKey={onSaveEditorKey}
          onClearKey={onClearEditorKey}
        />
      )}
    </article>
  );
}

function ProviderModelDraftPicker({
  draft,
  busy,
  fetching,
  onToggle,
  onSelectAll,
  onClear,
  onCancel,
  onSave,
}: {
  draft: ProviderModelDraft;
  busy: boolean;
  fetching: boolean;
  onToggle: (model: string) => void;
  onSelectAll: () => void;
  onClear: () => void;
  onCancel: () => void;
  onSave: () => void;
}) {
  const t = useT();
  const [query, setQuery] = useState("");
  const selected = new Set(draft.selected);
  const q = query.trim().toLowerCase();
  const visibleCandidates = q
    ? draft.candidates.filter((model) => model.toLowerCase().includes(q))
    : draft.candidates;
  const disabled = busy || fetching;

  return (
    <div className="provider-model-draft">
      <div className="provider-model-draft__head">
        <div>
          <div className="provider-card-block__label">{t("settings.modelCandidates")}</div>
          <span>{t("settings.modelCandidatesSelected", { n: draft.selected.length })}</span>
        </div>
        <div className="provider-model-draft__tools">
          <button type="button" className="btn btn--small" disabled={disabled || draft.selected.length === draft.candidates.length} onClick={onSelectAll}>
            {t("settings.selectAllModels")}
          </button>
          <button type="button" className="btn btn--small" disabled={disabled || draft.selected.length === 0} onClick={onClear}>
            {t("settings.clearModelSelection")}
          </button>
        </div>
      </div>
      <input
        className="mem-input provider-model-draft__search"
        placeholder={t("settings.modelCandidateSearch")}
        value={query}
        disabled={disabled}
        onChange={(e) => setQuery(e.target.value)}
      />
      <div className="provider-model-draft__list" role="list" aria-label={t("settings.modelCandidates")}>
        {visibleCandidates.length > 0 ? visibleCandidates.map((model) => (
          <label className="provider-model-draft__option" key={model}>
            <input
              type="checkbox"
              checked={selected.has(model)}
              disabled={disabled}
              onChange={() => onToggle(model)}
            />
            <span>{model}</span>
          </label>
        )) : (
          <div className="provider-model-draft__empty">{t("settings.noMatchingCandidateModels")}</div>
        )}
      </div>
      <div className="provider-model-draft__actions">
        <button type="button" className="btn btn--small" disabled={disabled} onClick={onCancel}>
          {t("common.cancel")}
        </button>
        <button type="button" className="btn btn--primary btn--small" disabled={disabled || draft.selected.length === 0} onClick={onSave}>
          {t("settings.saveEnabledModels")}
        </button>
      </div>
    </div>
  );
}

function providerAccessGroups(providers: ProviderView[], t: ReturnType<typeof useT>): ProviderAccessGroup[] {
  const groups = new Map<string, ProviderAccessGroup>();
  for (const p of providers) {
    const id = providerGroupID(p);
    const builtIn = id.startsWith("builtin:");
    const existing = groups.get(id);
    if (existing) {
      existing.providers.push(p);
      existing.keySet = existing.keySet || p.keySet;
      existing.models = uniqueStrings([...existing.models, ...p.models]);
      continue;
    }
    groups.set(id, {
      id,
      label: providerGroupLabel(p, t),
      description: providerGroupDescription(p, t),
      builtIn,
      providers: [p],
      apiKeyEnv: p.apiKeyEnv,
      keySet: p.keySet,
      baseUrl: p.baseUrl,
      kind: p.kind,
      models: uniqueStrings(p.models),
    });
  }
  return Array.from(groups.values());
}

function providerBaseHost(baseUrl: string): string {
  try {
    return new URL(baseUrl).hostname.toLowerCase();
  } catch {
    return "";
  }
}

function canonicalOfficialProviderName(name: string): string {
  switch (name.trim()) {
    case "moma":
      return "moma";
    default:
      return name.trim();
  }
}

function officialProviderKind(p: ProviderView): string {
  if (!p.builtIn) return "";
  const name = canonicalOfficialProviderName(p.name);
  const host = providerBaseHost(p.baseUrl);
  if (name === "moma" && host === "jiutian.10086.cn") return "moma";
  return "";
}

function providerGroupID(p: ProviderView): string {
  const official = officialProviderKind(p);
  if (official) return `builtin:${official}`;
  return `custom:${p.name}`;
}

function providerGroupLabel(p: ProviderView, t?: ReturnType<typeof useT>): string {
  const id = providerGroupID(p);
	if (id === "builtin:moma") return t ? t("settings.providerLabel.moma") : "MoMA";
  return p.name;
}

function providerGroupDescription(p: ProviderView, t: ReturnType<typeof useT>): string {
  const id = providerGroupID(p);
	if (id === "builtin:moma") return t("settings.providerDesc.moma");
  return p.baseUrl;
}

function uniqueStrings(values: string[]): string[] {
  const out: string[] = [];
  for (const value of values) {
    if (value && !out.includes(value)) out.push(value);
  }
  return out;
}

function apiKeyEnvFromProviderName(name: string): string {
  const stem = name
    .trim()
    .toUpperCase()
    .replace(/[^A-Z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
  return stem ? `${stem}_API_KEY` : "CUSTOM_API_KEY";
}

function ProviderEditor({
  initial,
  kinds,
  busy,
  onCancel,
  onSave,
  onSaveKey,
  onClearKey,
}: {
  initial?: ProviderView;
  kinds: string[];
  busy: boolean;
  onCancel: () => void;
  onSave: (p: ProviderView) => void;
  onSaveKey?: (apiKeyEnv: string, value: string) => Promise<void>;
  onClearKey?: (apiKeyEnv: string) => Promise<void>;
}) {
  const t = useT();
  const [name, setName] = useState(initial?.name ?? "");
  const [kind, setKind] = useState(initial?.kind ?? kinds[0] ?? "openai");
  const [baseUrl, setBaseUrl] = useState(initial?.baseUrl ?? "");
  const [models, setModels] = useState((initial?.models ?? []).join(", "));
  const [modelsUrl] = useState(initial?.modelsUrl ?? "");
  const [apiKeyEnv, setApiKeyEnv] = useState(initial?.apiKeyEnv ?? "");
  const [keyDraft, setKeyDraft] = useState("");
  // Empty when unset so the placeholder (and its "0 = default" hint) reads instead
  // of a bare "0"; saved back as 0.
  const [ctx, setCtx] = useState(initial?.contextWindow ? String(initial.contextWindow) : "");
  const [reasoningProtocol, setReasoningProtocol] = useState(normalizeReasoningProtocol(initial?.reasoningProtocol));
  const [supportedEfforts, setSupportedEfforts] = useState<string[]>(initial?.supportedEfforts ?? []);
  const [customEffortDraft, setCustomEffortDraft] = useState("");
  const [defaultEffort, setDefaultEffort] = useState(initial?.defaultEffort ?? "");
  const [fetchingModels, setFetchingModels] = useState(false);
  const [fetchStatus, setFetchStatus] = useState<string | null>(null);
  const [fetchErr, setFetchErr] = useState<string | null>(null);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const builtIn = initial?.builtIn ?? false;
  const isNewCustomProvider = !initial;

  // Offer the kinds the kernel actually registered; if the stored kind is a
  // legacy/unknown one, keep it as an option so editing doesn't silently change it.
  const kindOptions = kind && !kinds.includes(kind) ? [kind, ...kinds] : kinds;

  // Split supportedEfforts into the 5 canonical presets (for checkbox UI) and
  // any user-added custom names (rendered as removable chips). The preset order
  // is fixed; custom names keep insertion order.
  const presetEfforts = supportedEfforts.filter((e) => EFFORT_PRESETS.includes(e));
  const customEfforts = supportedEfforts.filter((e) => !EFFORT_PRESETS.includes(e));

  const togglePreset = (level: string) => {
    const has = presetEfforts.includes(level);
    const nextPresets = has ? presetEfforts.filter((e) => e !== level) : [...presetEfforts, level];
    setSupportedEfforts([...nextPresets, ...customEfforts]);
    // If the removed preset was the default, fall back to "auto" (empty string).
    if (has && defaultEffort === level) setDefaultEffort("");
  };

  const addCustomEffort = () => {
    const v = customEffortDraft.trim().toLowerCase();
    if (!v || supportedEfforts.includes(v)) {
      setCustomEffortDraft("");
      return;
    }
    setSupportedEfforts([...presetEfforts, ...customEfforts, v]);
    setCustomEffortDraft("");
  };

  const removeCustomEffort = (level: string) => {
    setSupportedEfforts(supportedEfforts.filter((e) => e !== level));
    if (defaultEffort === level) setDefaultEffort("");
  };

  const fetchModels = async () => {
    setFetchingModels(true);
    setFetchStatus(null);
    setFetchErr(null);
    try {
      const effectiveApiKeyEnv = apiKeyEnv.trim() || apiKeyEnvFromProviderName(name);
      if (!apiKeyEnv.trim()) setApiKeyEnv(effectiveApiKeyEnv);
      if (keyDraft.trim()) await app.SetProviderKey(effectiveApiKeyEnv, keyDraft.trim());
      const fetched = await app.FetchProviderModels({
        name: name.trim() || t("settings.newProviderDraftName"),
        builtIn: initial?.builtIn ?? false,
        added: initial?.added ?? true,
        kind: kind.trim() || kinds[0] || "openai",
        baseUrl: baseUrl.trim(),
        modelsUrl,
        models: [],
        default: "",
        apiKeyEnv: effectiveApiKeyEnv,
        keySet: Boolean(keyDraft.trim()) || (initial?.keySet ?? false),
        contextWindow: Number(ctx) || 0,
        reasoningProtocol,
        supportedEfforts,
        defaultEffort,
      });
      if (fetched.length === 0) throw new Error(t("settings.fetchModelsEmpty"));
      setModels(fetched.join(", "));
      if (keyDraft.trim()) setKeyDraft("");
      setDefaultEffort((v) => v);
      setFetchStatus(t("settings.fetchModelsSuccess", { n: fetched.length }));
    } catch (e) {
      setFetchErr(String((e as Error)?.message ?? e));
    } finally {
      setFetchingModels(false);
    }
  };

  const save = async () => {
    const ms = models
      .split(",")
      .map((m) => m.trim())
      .filter(Boolean);
    const effectiveApiKeyEnv = apiKeyEnv.trim() || apiKeyEnvFromProviderName(name);
    if (keyDraft.trim()) await app.SetProviderKey(effectiveApiKeyEnv, keyDraft.trim());
    onSave({
      name: name.trim(),
      builtIn: initial?.builtIn ?? false,
      added: initial?.added ?? true,
      kind: kind.trim() || kinds[0] || "openai",
      baseUrl: baseUrl.trim(),
      models: ms,
      default: ms[0] ?? "",
      apiKeyEnv: effectiveApiKeyEnv,
      modelsUrl,
      keySet: Boolean(keyDraft.trim()) || (initial?.keySet ?? false),
      contextWindow: Number(ctx) || 0,
      reasoningProtocol,
      supportedEfforts,
      // Clear the stored default if no levels are selected; the backend's
      // NormalizeEffort would otherwise silently ignore an unsupported value.
      defaultEffort: supportedEfforts.length > 0 ? defaultEffort : "",
    });
  };

  if (builtIn) {
    const keyEnv = initial?.apiKeyEnv.trim() ?? "";
    return (
      <div className="provider-editor provider-editor--builtin provider-editor--key-only">
        {initial && onSaveKey && keyEnv && (
          <>
            <div className="provider-key-status provider-key-status--managed provider-key-status--compact">
              <span>{initial.keySet ? t("settings.configuredKey", { env: keyEnv }) : t("settings.notConfiguredKey", { env: keyEnv })}</span>
              {initial.keySet && onClearKey && (
                <InlineConfirmButton
                  label={t("settings.clearKey")}
                  confirmLabel={t("settings.confirmClearKey")}
                  cancelLabel={t("common.cancel")}
                  disabled={busy}
                  danger
                  onConfirm={() => onClearKey(keyEnv)}
                />
              )}
            </div>
            <KeyField
              apiKeyEnv={keyEnv}
              busy={busy}
              keySet={initial.keySet}
              onSet={(env, value) => onSaveKey(env, value)}
            />
          </>
        )}
      </div>
    );
  }

  const modelNames = models
    .split(",")
    .map((m) => m.trim())
    .filter(Boolean);
  const canFetch = Boolean(name.trim() && baseUrl.trim() && (keyDraft.trim() || apiKeyEnv.trim()));

  const protocolField = initial ? (
    <select className="mem-select" value={kind} onChange={(e) => setKind(e.target.value)}>
      {kindOptions.map((k) => (
        <option key={k} value={k}>
          {k === "openai" ? t("settings.providerProtocolOpenAI") : k}
        </option>
      ))}
    </select>
  ) : (
    <div className="provider-readonly-field provider-readonly-field--stacked" aria-readonly="true">
      <strong>{t("settings.providerProtocolOpenAI")}</strong>
      <span>{t("settings.providerProtocolOpenAIHint")}</span>
    </div>
  );

  const advancedFields = (
    <details className="provider-editor-advanced" open={advancedOpen} onToggle={(e) => setAdvancedOpen(e.currentTarget.open)}>
      <summary>{t("settings.providerAdvancedSettings")}</summary>
      <div className="provider-editor-advanced__body">
        <label className="set-label">{t("settings.providerApiKeyEnv")}</label>
        <input
          className="mem-input"
          placeholder={apiKeyEnvFromProviderName(name)}
          value={apiKeyEnv}
          onChange={(e) => setApiKeyEnv(e.target.value)}
        />
        <div className="mem-hint">{t("settings.providerApiKeyEnvHint")}</div>
        <label className="set-label">{t("settings.providerContextWindow")}</label>
        <input className="mem-input" placeholder={t("settings.contextWindowPlaceholder")} value={ctx} onChange={(e) => setCtx(e.target.value)} inputMode="numeric" />
        <div className="mem-hint">{t("settings.contextWindowHint")}</div>
        <label className="set-label">{t("settings.reasoningProtocol")}</label>
        <select className="mem-select" value={reasoningProtocol} onChange={(e) => setReasoningProtocol(e.target.value)}>
          {REASONING_PROTOCOLS.map((protocol) => (
            <option key={protocol || "auto"} value={protocol}>
              {reasoningProtocolLabel(protocol, t)}
            </option>
          ))}
        </select>
        <div className="mem-hint">{t("settings.reasoningProtocolHint")}</div>
        <label className="set-label">{t("settings.supportedEfforts")}</label>
        {EFFORT_PRESETS.map((level) => (
          <label key={level} className="set-check">
            <input
              type="checkbox"
              checked={presetEfforts.includes(level)}
              onChange={() => togglePreset(level)}
            />
            {effortLabel(level, t)}
          </label>
        ))}
        <div className="set-row">
          <input
            className="mem-input set-grow"
            placeholder={t("settings.supportedEffortsCustomPlaceholder")}
            value={customEffortDraft}
            onChange={(e) => setCustomEffortDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                addCustomEffort();
              }
            }}
          />
          <button
            type="button"
            className="btn btn--small"
            disabled={
              !customEffortDraft.trim() || supportedEfforts.includes(customEffortDraft.trim().toLowerCase())
            }
            onClick={addCustomEffort}
          >
            {t("common.add")}
          </button>
        </div>
        {customEfforts.length > 0 && (
          <div className="set-rules__chips">
            {customEfforts.map((level) => (
              <span className="set-rule" key={level}>
                {level}
                <Tooltip label={t("common.delete")}>
                  <button
                    type="button"
                    className="set-rule__x"
                    disabled={busy}
                    onClick={() => removeCustomEffort(level)}
                  >
                    ×
                  </button>
                </Tooltip>
              </span>
            ))}
          </div>
        )}
        <div className="mem-hint">{t("settings.supportedEffortsHint")}</div>
        <label className="set-label">{t("settings.defaultEffort")}</label>
        {supportedEfforts.length > 0 ? (
          <select
            className="mem-select"
            value={defaultEffort}
            onChange={(e) => setDefaultEffort(e.target.value)}
          >
            <option value="">{t("settings.defaultEffortAuto")}</option>
            {supportedEfforts.map((level) => (
              <option key={level} value={level}>
                {effortLabel(level, t)}
              </option>
            ))}
          </select>
        ) : (
          <select className="mem-select" value="" disabled>
            <option value="">{t("settings.defaultEffortAuto")}</option>
          </select>
        )}
        <div className="mem-hint">{t("settings.defaultEffortHint")}</div>
      </div>
    </details>
  );

  return (
    <div className={`provider-editor${isNewCustomProvider ? " provider-editor--wizard" : ""}`}>
      <label className="set-label">{t("settings.customProviderName")}</label>
      <input className="mem-input" placeholder={t("settings.customProviderNamePlaceholder")} value={name} onChange={(e) => setName(e.target.value)} disabled={!!initial} />
      <label className="set-label">{t("settings.providerProtocol")}</label>
      {protocolField}
      <label className="set-label">{t("settings.providerBaseUrlLabel")}</label>
      <div className="provider-presets">
        {COMMON_PROVIDER_PRESETS.map((preset) => (
          <button
            key={preset.label}
            type="button"
            className={"set-seg__btn provider-presets__btn" + (baseUrl === preset.baseUrl ? " set-seg__btn--on" : "")}
            onClick={() => {
              setBaseUrl(preset.baseUrl);
              if (preset.context) setCtx(String(preset.context));
            }}
          >
            {preset.label}
          </button>
        ))}
      </div>
      <input className="mem-input" placeholder={t("settings.providerBaseUrl")} value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} />
      {!initial && (
        <>
          <label className="set-label">{t("settings.providerKey")}</label>
          <input
            className="mem-input"
            type="password"
            placeholder={t("settings.providerKeyPlaceholder")}
            value={keyDraft}
            onChange={(e) => setKeyDraft(e.target.value)}
          />
        </>
      )}
      {initial && onSaveKey && apiKeyEnv.trim() && (
        <>
          <label className="set-label">{t("settings.providerKey")}</label>
          <KeyField
            apiKeyEnv={apiKeyEnv.trim()}
            busy={busy || fetchingModels}
            keySet={initial.keySet}
            onSet={(env, value) => onSaveKey(env, value)}
          />
        </>
      )}
      <div className="provider-model-fetch-row">
        <button
          type="button"
          className="btn btn--small"
          disabled={busy || fetchingModels || !canFetch}
          onClick={() => void fetchModels()}
        >
          {fetchingModels ? t("settings.fetchingModels") : t("settings.testFetchModels")}
        </button>
        <span>{t("settings.testFetchModelsHint")}</span>
      </div>
      {fetchStatus && <div className="provider-fetch-status provider-fetch-status--ok">{fetchStatus}</div>}
      {fetchErr && <div className="provider-fetch-status provider-fetch-status--error">{fetchErr}</div>}
      {modelNames.length > 0 && (
        <div className="provider-card-block">
          <div className="provider-card-block__label">{t("settings.availableModels")}</div>
          <div className="provider-model-chips">
            {modelNames.slice(0, 8).map((model) => (
              <span className="provider-model-chip" key={model}>{model}</span>
            ))}
            {modelNames.length > 8 && (
              <span className="provider-model-chip provider-model-chip--more">{t("settings.moreModels", { n: modelNames.length - 8 })}</span>
            )}
          </div>
        </div>
      )}
      <label className="set-label">{t("settings.manualModels")}</label>
      <input className="mem-input" placeholder={t("settings.providerModels")} value={models} onChange={(e) => setModels(e.target.value)} />
      <div className="mem-hint">{t("settings.manualModelsHint")}</div>
      {advancedFields}
      <div className="prov-card__actions">
        <button className="btn btn--small" onClick={onCancel} disabled={busy}>
          {t("common.cancel")}
        </button>
        <button className="btn btn--primary btn--small" onClick={() => void save()} disabled={busy || !name.trim() || !baseUrl.trim() || !models.trim()}>
          {t("common.save")}
        </button>
      </div>
    </div>
  );
}

function KeyField({
  apiKeyEnv,
  busy,
  keySet = false,
  onSet,
}: {
  apiKeyEnv: string;
  busy: boolean;
  keySet?: boolean;
  onSet: (apiKeyEnv: string, value: string) => Promise<void>;
}) {
  const t = useT();
  const [val, setVal] = useState("");
  if (!apiKeyEnv) return null;
  return (
    <div className="set-key">
      <input
        className="mem-input"
        type="password"
        placeholder={t(keySet ? "settings.updateKey" : "settings.setKey", { env: apiKeyEnv })}
        value={val}
        onChange={(e) => setVal(e.target.value)}
      />
      <button
        className="btn btn--small"
        disabled={busy || !val.trim()}
        onClick={() => {
          void onSet(apiKeyEnv, val.trim());
          setVal("");
        }}
      >
        {t(keySet ? "settings.updateKeyAction" : "settings.saveKey")}
      </button>
    </div>
  );
}

function PermissionsSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  return (
    <>
    <SettingsSection title={t("settings.permissions")} description={t("settings.permissionsModeHint")}>
      <SettingsField label={t("settings.writerMode")}>
        <select
          className="mem-select set-grow"
          value={s.permissions.mode}
          disabled={busy}
          onChange={(e) => void apply(() => app.SetPermissionMode(e.target.value))}
        >
          <option value="ask">{t("settings.modeAsk")}</option>
          <option value="allow">{t("settings.modeAllow")}</option>
          <option value="deny">{t("settings.modeDeny")}</option>
        </select>
      </SettingsField>
    </SettingsSection>
    <SettingsSection title={t("settings.permissionRules")} description={t("settings.ruleForm")}>
      <div className="set-rules-grid">
        {(["deny", "ask", "allow"] as const).map((list) => (
          <RuleList
            key={list}
            list={list}
            rules={s.permissions[list]}
            busy={busy}
            onAdd={(rule) => apply(() => app.AddPermissionRule(list, rule))}
            onRemove={(rule) => apply(() => app.RemovePermissionRule(list, rule))}
          />
        ))}
      </div>
    </SettingsSection>
    </>
  );
}

function RuleList({
  list,
  rules,
  busy,
  onAdd,
  onRemove,
}: {
  list: string;
  rules: string[];
  busy: boolean;
  onAdd: (rule: string) => Promise<void>;
  onRemove: (rule: string) => Promise<void>;
}) {
  const t = useT();
  const [draft, setDraft] = useState("");
  const add = () => {
    const r = draft.trim();
    if (r) {
      void onAdd(r);
      setDraft("");
    }
  };
  return (
    <div className="set-rules">
      <div className="set-rules__head">
        <div className="set-rules__label">{ruleListLabel(list, t)}</div>
        {ruleListHint(list, t) && <div className="set-rules__hint">{ruleListHint(list, t)}</div>}
      </div>
      <div className="set-rules__chips">
        {rules.length === 0 && <span className="mem-empty">{t("common.none")}</span>}
        {rules.map((r) => (
          <span className="set-rule" key={r}>
            {r}
            <Tooltip label={t("common.delete")}>
              <button className="set-rule__x" disabled={busy} onClick={() => void onRemove(r)}>
                ✕
              </button>
            </Tooltip>
          </span>
        ))}
      </div>
      <div className="set-rules__add">
        <input
          className="mem-input"
          placeholder={t("settings.addRule", { list: ruleListLabel(list, t) })}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") add();
          }}
        />
        <button className="btn btn--small" disabled={busy || !draft.trim()} onClick={add}>
          {t("common.add")}
        </button>
      </div>
    </div>
  );
}

function ruleListLabel(list: string, t: ReturnType<typeof useT>): string {
  switch (list) {
    case "deny":
      return t("settings.ruleDeny");
    case "ask":
      return t("settings.ruleAsk");
    case "allow":
      return t("settings.ruleAllow");
    case "allow_write":
      return t("settings.ruleAllowWrite");
    default:
      return list;
  }
}

function ruleListHint(list: string, t: ReturnType<typeof useT>): string {
  switch (list) {
    case "deny":
      return t("settings.ruleDenyHint");
    case "ask":
      return t("settings.ruleAskHint");
    case "allow":
      return t("settings.ruleAllowHint");
    case "allow_write":
      return t("settings.ruleAllowWriteHint");
    default:
      return "";
  }
}

function SandboxSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  const sb = s.sandbox;
  const [root, setRoot] = useState(sb.workspaceRoot);
  const set = (next: Partial<typeof sb>) =>
    apply(() => app.SetSandbox(next.bash ?? sb.bash, next.network ?? sb.network, next.workspaceRoot ?? sb.workspaceRoot, next.allowWrite ?? sb.allowWrite));

  return (
    <SettingsSection title={t("settings.sandboxTitle")}>
      <SettingsField label={t("settings.bashSandbox")}>
        <select className="mem-select set-grow" value={sb.bash} disabled={busy} onChange={(e) => void set({ bash: e.target.value })}>
          <option value="enforce">{t("settings.bashEnforce")}</option>
          <option value="off">{t("settings.bashOff")}</option>
        </select>
      </SettingsField>
      <SettingsField label={t("settings.allowNetwork")}>
        <label className="set-check set-check--inline">
          <input type="checkbox" checked={sb.network} disabled={busy} onChange={(e) => void set({ network: e.target.checked })} />
          {t("settings.allowNetwork")}
        </label>
      </SettingsField>
      <SettingsField label={t("settings.workspaceRoot")}>
        <div className="set-input-browse">
          <input
            className="mem-input set-grow"
            placeholder={t("settings.workspaceDefault")}
            value={root}
            disabled={busy}
            onChange={(e) => setRoot(e.target.value)}
            onBlur={() => root !== sb.workspaceRoot && void set({ workspaceRoot: root })}
          />
          <button
            type="button"
            className="btn btn--small set-input-browse__btn"
            disabled={busy}
            onClick={async () => {
              const dir = await app.PickDirectory(t("settings.workspaceRoot"));
              if (dir) {
                setRoot(dir);
                void set({ workspaceRoot: dir });
              }
            }}
          >
            {t("settings.browse")}
          </button>
        </div>
      </SettingsField>
      <RuleList
        list="allow_write"
        rules={sb.allowWrite}
        busy={busy}
        onAdd={(d) => set({ allowWrite: [...sb.allowWrite, d] })}
        onRemove={(d) => set({ allowWrite: sb.allowWrite.filter((x) => x !== d) })}
      />
    </SettingsSection>
  );
}

// Visual-style metadata for the appearance theme cards. The two surface
// swatches + accent are read from CSS variables at render time so they always
// reflect the live token values for the currently-resolved light/dark mode.
const THEME_STYLE_META: Record<ThemeStyle, { name: string; zh: DictKey; note: DictKey; desc: DictKey }> = {
  graphite: { name: "Graphite", zh: "settings.style.graphite.zh", note: "settings.style.graphite.note", desc: "settings.style.graphite.desc" },
  aurora: { name: "Aurora", zh: "settings.style.aurora.zh", note: "settings.style.aurora.note", desc: "settings.style.aurora.desc" },
  slate: { name: "Slate", zh: "settings.style.slate.zh", note: "settings.style.slate.note", desc: "settings.style.slate.desc" },
  carbon: { name: "Carbon", zh: "settings.style.carbon.zh", note: "settings.style.carbon.note", desc: "settings.style.carbon.desc" },
  nocturne: { name: "Nocturne", zh: "settings.style.nocturne.zh", note: "settings.style.nocturne.note", desc: "settings.style.nocturne.desc" },
  amber: { name: "Amber", zh: "settings.style.amber.zh", note: "settings.style.amber.note", desc: "settings.style.amber.desc" },
};

function AppearanceSection({
  theme,
  themeStyle,
  textSize,
  fontFamily,
  onTheme,
  onThemeStyle,
  onTextSize,
  onFontFamily,
}: {
  theme: Theme;
  themeStyle: ThemeStyle;
  textSize: TextSize;
  fontFamily: FontFamily;
  onTheme: (t: Theme) => void;
  onThemeStyle: (style: ThemeStyle) => void;
  onTextSize: (size: TextSize) => void;
  onFontFamily: (font: FontFamily) => void;
}) {
  const t = useT();
  const themeOptions: Theme[] = ["auto", "light", "dark"];
  return (
    <SettingsSection title={t("settings.appearance")}>
      <SettingsField label={t("settings.theme")}>
        <div className="set-seg">
          {themeOptions.map((opt) => (
            <button
              key={opt}
              className={`set-seg__btn${theme === opt ? " set-seg__btn--on" : ""}`}
              onClick={() => onTheme(opt)}
            >
              {themeName(opt, t)}
            </button>
          ))}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.themeStyle")} stacked>
        <div className="theme-card-grid">
          {THEME_STYLES.map((opt) => {
            const meta = THEME_STYLE_META[opt];
            const selected = themeStyle === opt;
            return (
              <button
                key={opt}
                type="button"
                role="radio"
                aria-checked={selected}
                className={`theme-card${selected ? " theme-card--on" : ""}`}
                onClick={() => onThemeStyle(opt)}
              >
                <span className="theme-card__head">
                  <span className="theme-card__name">
                    {meta.name} <span className="theme-card__zh">{t(meta.zh)}</span>
                  </span>
                  <span className="theme-card__tag">{t(meta.note)}</span>
                </span>
                <span className="theme-card__swatches" data-theme-style-card={opt}>
                  <span className="theme-card__swatch theme-card__swatch--bg" />
                  <span className="theme-card__swatch theme-card__swatch--surface" />
                  <span className="theme-card__swatch theme-card__swatch--accent" />
                </span>
                <span className="theme-card__desc">{t(meta.desc)}</span>
                <span className="theme-card__check" aria-hidden="true">
                  <Check size={13} strokeWidth={3} />
                </span>
              </button>
            );
          })}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.textSize")}>
        <div className="set-seg">
          {TEXT_SIZES.map((size) => (
            <button
              key={size}
              className={`set-seg__btn${textSize === size ? " set-seg__btn--on" : ""}`}
              onClick={() => onTextSize(size)}
            >
              {textSizeName(size, t)}
            </button>
          ))}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.fontFamily")}>
        <div className="set-seg">
          {FONT_FAMILIES.map((font) => (
            <button
              key={font}
              className={`set-seg__btn${fontFamily === font ? " set-seg__btn--on" : ""}`}
              onClick={() => onFontFamily(font)}
            >
              {fontFamilyName(font, t)}
            </button>
          ))}
        </div>
      </SettingsField>
    </SettingsSection>
  );
}

function themeName(theme: Theme, t: ReturnType<typeof useT>): string {
  switch (theme) {
    case "auto":
      return t("settings.themeAuto");
    case "light":
      return t("settings.themeLight");
    case "dark":
      return t("settings.themeDark");
  }
}

function textSizeName(size: TextSize, t: ReturnType<typeof useT>): string {
  switch (size) {
    case "small":
      return t("settings.textSizeSmall");
    case "default":
      return t("settings.textSizeDefault");
    case "large":
      return t("settings.textSizeLarge");
    case "xlarge":
      return t("settings.textSizeXLarge");
  }
}

function fontFamilyName(font: FontFamily, t: ReturnType<typeof useT>): string {
  switch (font) {
    case "system":
      return t("settings.fontFamilySystem");
    case "yahei":
      return t("settings.fontFamilyYaHei");
    case "pingfang":
      return t("settings.fontFamilyPingFang");
    case "noto":
      return t("settings.fontFamilyNoto");
  }
}

const MB = 1024 * 1024;
const mb = (n: number) => (n / MB).toFixed(1);

// UpdatesSection is the manual side of the auto-updater: it shows the startup
// check preference, running version, and a Check button, then the same state
// machine the top banner uses (useUpdater) — available → install/download, with
// progress and errors inline.
function UpdatesSection({
  configPath,
  checkUpdates,
  telemetry,
  settingsBusy,
  applySettings,
}: {
  configPath: string;
  checkUpdates: boolean;
  telemetry: boolean;
  settingsBusy: boolean;
  applySettings: (fn: () => Promise<void>) => Promise<void>;
}) {
  const t = useT();
  const { status, check, apply: applyUpdate } = useUpdater();
  const [version, setVersion] = useState("");
  useEffect(() => {
    app.Version().then(setVersion).catch(() => {});
  }, []);

  const updaterBusy =
    status.kind === "checking" || status.kind === "downloading" || status.kind === "verifying" || status.kind === "applying";

  return (
    <SettingsSection title={t("updater.title")}>
      <SettingsField
        className="settings-field--wide-copy"
        label={t("updater.autoCheckLabel")}
        hint={t("updater.autoCheckHint")}
      >
        <ToggleSegment
          value={checkUpdates}
          disabled={settingsBusy}
          onChange={(enabled) => void applySettings(() => app.SetDesktopCheckUpdates(enabled))}
        />
      </SettingsField>
      <SettingsField
        className="settings-field--wide-copy"
        label={t("settings.telemetryLabel")}
        hint={t("settings.telemetryHint")}
      >
        <ToggleSegment
          value={telemetry}
          disabled={settingsBusy}
          onChange={(enabled) => void applySettings(() => app.SetDesktopTelemetry(enabled))}
        />
      </SettingsField>
      <SettingsField label={t("updater.currentVersion", { v: version || "…" })}>
        <button className="btn btn--small" disabled={updaterBusy} onClick={() => void check()}>
          {status.kind === "checking" ? t("updater.checking") : t("updater.checkButton")}
        </button>
      </SettingsField>
      {status.kind === "upToDate" && <div className="mem-hint">{t("updater.upToDate")}</div>}
      {status.kind === "available" && (
        <>
          <SettingsField label={t("updater.available", { v: status.info.latest })}>
            <button className="btn btn--primary btn--small" onClick={() => applyUpdate(status.info)}>
              {status.info.canSelfUpdate ? t("updater.installNow") : t("updater.goToDownload")}
            </button>
          </SettingsField>
          {!status.info.canSelfUpdate && <div className="mem-hint">{t("updater.macHint")}</div>}
        </>
      )}
      {status.kind === "downloading" && (
        <div className="mem-hint">
          {t("updater.downloading", {
            done: mb(status.received),
            total: mb(status.total),
            pct: status.total > 0 ? Math.round((status.received / status.total) * 100) : 0,
          })}
        </div>
      )}
      {status.kind === "verifying" && <div className="mem-hint">{t("updater.verifying")}</div>}
      {status.kind === "applying" && <div className="mem-hint">{t("updater.applying")}</div>}
      {status.kind === "done" && <div className="mem-hint">{t("updater.done")}</div>}
      {status.kind === "error" && <div className="banner banner--error">{t("updater.failed", { msg: status.message })}</div>}
      {configPath && (
        <Tooltip label={configPath} fill block className="mem-hint settings-config-path">
          {t("settings.config", { path: configPath })}
        </Tooltip>
      )}
    </SettingsSection>
  );
}


/** Collapsible optional-module card with a toggle switch. When disabled the
 *  body (config fields) is hidden and the card appears muted. */
function OptionalModule({
  title,
  description,
  enabled,
  onToggle,
  children,
  statusDot,
}: {
  title: string;
  description: string;
  enabled: boolean;
  onToggle: (v: boolean) => void;
  children: ReactNode;
  // statusDot overrides the default "configured" dot. When provided (e.g. the
  // mail card's live green/red connection indicator), it replaces the orange
  // dot entirely; the default dot still applies to other cards.
  statusDot?: ReactNode;
}) {
  // The switch toggles the config panel open/closed. `enabled` (derived from
  // whether the module has been configured) only drives the switch's checked
  // visual + a "configured" highlight — it no longer gates the panel body,
  // which is what previously made "turn on" a no-op (the toggle* helpers had
  // no `on=true` branch, so the body never showed and the checkbox could never
  // light up). `expanded` defaults to the configured state so already-set-up
  // modules open on first paint; the user can collapse/expand at will, and
  // collapsing runs the parent's clear-fields cleanup via onToggle(false).
  const [expanded, setExpanded] = useState(enabled);
  return (
    <div className="optional-module">
      <div className="optional-module__head">
        <div className="optional-module__copy">
          <div className="optional-module__title">
            {title}
            {statusDot ?? (enabled && <span className="optional-module__configured-dot" title="" />)}
          </div>
          <div className="optional-module__desc">{description}</div>
        </div>
        <label className="cap-switch">
          <input
            type="checkbox"
            checked={expanded || enabled}
            onChange={(e) => {
              const on = e.target.checked;
              setExpanded(on);
              if (!on) onToggle(false); // collapse = clear configured fields
            }}
          />
          <span className="cap-switch__track" />
        </label>
      </div>
      {expanded && <div className="optional-module__body">{children}</div>}
    </div>
  );
}


// browserDisplayName maps a browser executable path to a friendly product
// name ("chrome.exe" → "Chrome"). Returns "" for an empty path (nothing
// detected). Used by the coWork browser detect button so the hint line still
// reads "Chrome detected" rather than a raw .exe path, while browserPath holds
// the actual path that gets persisted.
function browserDisplayName(path: string): string {
  const base = path.split(/[\\/]/).pop()?.toLowerCase() ?? "";
  if (base.includes("chrome")) return "Chrome";
  if (base.includes("msedge") || base.includes("edge")) return "Edge";
  if (base.includes("brave")) return "Brave";
  return base ? base.replace(/\.exe$/, "") : "";
}

function CoWorkSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  const [draft, setDraft] = useState(() => {
    const base = s.cowork ?? {
      browserPath: "", embeddingModel: "",
      pptActiveTemplate: "", pptTemplates: [], pptTemplateDir: "",
      smtpPassword: "", imapPassword: "", smtpPasswordSet: false, imapPasswordSet: false, detectedBrowser: "",
      screenshotEnabled: false, screenshotHotkey: "Ctrl+Shift+S", screenshotVlmModel: "qwen/qwen3.6-27b",
      estopHotkey: "Ctrl+Shift+Pause",
    };
    // Default mail provider = 139 (China Mobile). Only when the user hasn't
    // configured mail yet (no saved SMTP host); a saved config — including one
    // the user deliberately cleared — is always respected. The useState
    // initializer runs once on mount, so later toggles/presets aren't affected.
    const mailConfigured = !!(base.smtp?.host);
    return {
      ...base,
      // Legacy single-account fields default to BLANK (not 139) — the multi-
      // account list is the real UI now, and hardcoding a provider here would
      // mislead users adding non-139 mailboxes. These fields only mirror the
      // Default account from the backend, so empty is the correct neutral seed.
      smtp: mailConfigured && base.smtp
        ? base.smtp
        : { host: "", port: 0, from: "", username: "", passwordEnv: "COWORK_SMTP_PASSWORD", useTLS: false, encryptionMode: "" },
      imap: mailConfigured && base.imap
        ? base.imap
        : { host: "", port: 0, username: "", passwordEnv: "COWORK_IMAP_PASSWORD" },
      // Multi-account list drives the mail UI when present. Mirrored onto
      // smtp/imap by the backend for legacy paths.
      emailAccounts: base.emailAccounts ?? [],
      allowHeadlessEmail: base.allowHeadlessEmail ?? false,
    };
  });
  
  const [browserDetecting, setBrowserDetecting] = useState(false);
  const [recordingHotkey, setRecordingHotkey] = useState(false);
  const [recordingEStopHotkey, setRecordingEStopHotkey] = useState(false);
  // draftRef mirrors the latest draft so async commit handlers (onBlur firing
  // right after a setDraft) read the post-update value, not the stale render
  // closure. Without this, typing into the password field then blurring could
  // commit a draft that predates the keystroke — the password never persisted.
  const draftRef = useRef(draft);
  useEffect(() => { draftRef.current = draft; }, [draft]);
  // dirtyRef tracks unsaved edits. Replaces the fragile JSON.stringify(draft)
  // vs JSON.stringify(s.cowork) comparison, which broke for mail because the
  // password field is write-only (present in draft, absent in the loaded view),
  // so the two never stringified equal and saves were either skipped or fired
  // with stale data. Mutations set it true; it's cleared right before a save
  // dispatch (not on apply completion, since the user may edit again while the
  // save is in flight and we must not lose that edit's dirty bit).
  const dirtyRef = useRef(false);
  // Mail connection status per account, probed after the user saves. Keyed by
  // account name; legacy single-account path uses "" (the Default account).
  // "idle" = not yet probed; "checking" = probe in flight; "ok" = IMAP login
  // succeeded; "error" = connection/auth failed. Drives the green/red dot on
  // each mail card. Not polled — one probe per save.
  const [mailStatus, setMailStatus] = useState<Record<string, "idle" | "checking" | "ok" | "error">>({});
  const [mailMessage, setMailMessage] = useState<Record<string, string>>({});

  // commitDraft persists the given draft snapshot. Callers passing an explicit
  // `next` have already decided to save, so we don't gate on dirtyRef. dirtyRef
  // is cleared before the dispatch so that an edit made during the in-flight
  // save re-marks it (and triggers another save on the next blur). The old
  // JSON.stringify equality check was removed because the mail password is
  // write-only — present in draft, absent in the loaded view — so the two never
  // stringified equal and saves were skipped.
  const commitDraft = (next: typeof draft) => {
    dirtyRef.current = false;
    void apply(() => app.SetCoWorkSettings(next));
  };
  // commitCurrent flushes the latest draft, but only if something actually
  // changed since the last save (dirtyRef). The ref read ensures an onBlur
  // firing right after a setDraft sees the updated value, not the closure's.
  const commitCurrent = () => {
    if (!dirtyRef.current) return;
    commitDraft(draftRef.current);
  };

  // probeMail tests a saved mailbox's IMAP login and updates that account's
  // status dot. `skey` is the status-map key (the account's list index, stable
  // across renames); `name` is what the backend resolves (empty = Default).
  // Skips when no mailbox is configured so we don't probe a non-existent
  // account. Called both after a save and from the explicit "测试连接" button.
  const probeMail = async (skey: string, name: string) => {
    const d = draftRef.current;
    const acct = (d.emailAccounts ?? []).find(a => a.name === name);
    if (name === "" ? (!d.smtp?.username && !d.imap?.host)
                    : (!acct?.smtp?.host && !acct?.imap?.host)) {
      setMailStatus(m => ({ ...m, [skey]: "idle" }));
      return;
    }
    setMailStatus(m => ({ ...m, [skey]: "checking" }));
    try {
      const r: MailProbeResult = await app.ProbeMailAccount(name);
      if (r.status === "unconfigured") {
        setMailStatus(m => ({ ...m, [skey]: "idle" }));
      } else if (r.ok) {
        setMailStatus(m => ({ ...m, [skey]: "ok" }));
        setMailMessage(m => ({ ...m, [skey]: r.message || "连接正常" }));
      } else {
        setMailStatus(m => ({ ...m, [skey]: "error" }));
        setMailMessage(m => ({ ...m, [skey]: r.message || "连接失败" }));
      }
    } catch {
      // ProbeMailAccount resolves rather than rejects on failure, but defend
      // against a transport error so the dot doesn't get stuck on "checking".
      setMailStatus(m => ({ ...m, [skey]: "error" }));
      setMailMessage(m => ({ ...m, [skey]: "检测请求失败" }));
    }
  };
  // commitMailThenProbe: flush the latest draft (via ref — an onBlur right after
  // a setDraft must see the typed value), THEN probe so the status dot reflects
  // what was actually saved. Awaits the write so the probe reads the just-
  // persisted config. When nothing changed we still probe on demand so the
  // "测试连接" button works without an edit. `i` is the account index (status
  // key); `name` is the backend lookup name.
  const commitMailThenProbe = async (i: number, name: string) => {
    if (dirtyRef.current) {
      const next = draftRef.current;
      dirtyRef.current = false;
      await apply(() => app.SetCoWorkSettings(next));
    }
    void probeMail(String(i), name);
  };

  const checkBrowser = async () => {
    setBrowserDetecting(true);
    try {
      // CheckCoworkBrowser returns the detected executable PATH. Autofill it
      // into browserPath so the config actually persists — previously detection
      // only set the read-only detectedBrowser, which was never saved, so the
      // picked browser vanished on reopen. The user can still override by
      // editing the path field directly. Detection persists immediately.
      const path = await app.CheckCoworkBrowser();
      setDraft(d => {
        const next = {
          ...d,
          detectedBrowser: browserDisplayName(path),
          browserPath: path,
        };
        commitDraft(next);
        return next;
      });
    } finally {
      setBrowserDetecting(false);
    }
  };

  const openTemplateDir = async () => {
    try { await app.OpenPPTTemplateDir(); } catch { /* see backend log */ }
  };

  // Derive enabled state from draft values — empty = disabled.
  const browserOn = !!(draft.detectedBrowser || draft.browserPath);
  const pptOn = !!draft.pptActiveTemplate;
  // Mail is "on" when at least one account is configured (multi-account path)
  // or the legacy single-pair SMTP/IMAP has a host.
  const mailOn = (draft.emailAccounts?.some(a => a.smtp?.host || a.imap?.host))
    || !!(draft.smtp?.host) || !!(draft.imap?.host);
  const ragOn = !!draft.embeddingModel;

  // Toggle helpers: disabling clears related fields so the backend treats them
  // as "not configured", and persists immediately (no save button). Enabling
  // just expands the card (user fills in values, saved on blur/enter).
  const toggleBrowser = (on: boolean) => {
    if (!on) setDraft(d => { const n = { ...d, browserPath: "", detectedBrowser: "" }; commitDraft(n); return n; });
  };
  const togglePpt = (on: boolean) => {
    if (!on) setDraft(d => { const n = { ...d, pptActiveTemplate: "" }; commitDraft(n); return n; });
  };
  // Disabling mail clears all accounts AND the legacy single-pair fields.
  // Enabling when the list is empty seeds a 139-preset account so the user has
  // a card to fill in immediately (matches the pre-multiaccount UX where opening
  // mail revealed a 139-prefilled username/auth-code pair).
  const toggleMail = (on: boolean) => {
    if (!on) setDraft(d => {
      const n = {
        ...d,
        smtp: { ...d.smtp, host: "", port: 0, from: "", username: "" },
        imap: { ...d.imap, host: "", port: 0, username: "" },
        smtpPassword: "", imapPassword: "",
        emailAccounts: [],
      };
      dirtyRef.current = true;
      commitDraft(n);
      return n;
    });
    if (on && (draftRef.current.emailAccounts ?? []).length === 0) {
      newAccount();
    }
  };

  // --- Multi-account helpers. Each mutates draft.emailAccounts and persists. ---
  // newAccount inserts a blank account template. The server host/port are left
  // EMPTY (not pre-filled with 139) so the user can configure ANY mailbox — 139,
  // chinamobile.com enterprise, QQ, Gmail, etc. Names must be unique — backend
  // dedups by name; the user fills in a friendly handle.
  const newAccount = () => {
    setDraft(d => {
      const acct = {
        name: "",
        default: (d.emailAccounts ?? []).length === 0,
        smtp: { host: "", port: 0, from: "", username: "", passwordEnv: "", useTLS: false, encryptionMode: "" },
        imap: { host: "", port: 0, username: "", passwordEnv: "" },
        password: "",
        passwordSet: false,
      };
      const n = { ...d, emailAccounts: [...(d.emailAccounts ?? []), acct] };
      dirtyRef.current = true;
      commitDraft(n);
      return n;
    });
  };
  // patchAccount updates one account by index with a partial update. Marks the
  // draft dirty so the next blur/commit actually persists (the password field
  // is write-only, so a JSON-equality check can't detect this change).
  const patchAccount = (i: number, patch: Partial<typeof draft.emailAccounts[number]>) => {
    setDraft(d => {
      const list = [...(d.emailAccounts ?? [])];
      list[i] = { ...list[i], ...patch };
      const n = { ...d, emailAccounts: list };
      dirtyRef.current = true;
      return n; // don't commit yet — onBlur handles persistence so we don't
               // fire a save per keystroke while the user is still typing.
    });
  };
  // removeAccount drops one account and reassigns Default to the first survivor.
  const removeAccount = (i: number) => {
    setDraft(d => {
      const list = [...(d.emailAccounts ?? [])];
      const removedDefault = list[i]?.default;
      list.splice(i, 1);
      if (removedDefault && list.length > 0) list[0] = { ...list[0], default: true };
      const n = { ...d, emailAccounts: list };
      dirtyRef.current = true;
      commitDraft(n);
      return n;
    });
  };
  // setAccountDefault makes one account the sole Default.
  const setAccountDefault = (i: number) => {
    setDraft(d => {
      const list = (d.emailAccounts ?? []).map((a, idx) => ({ ...a, default: idx === i }));
      const n = { ...d, emailAccounts: list };
      dirtyRef.current = true;
      commitDraft(n);
      return n;
    });
  };
  const toggleRag = (on: boolean) => {
    if (!on) setDraft(d => { const n = { ...d, embeddingModel: "" }; commitDraft(n); return n; });
  };

  return (
    <>
      <SettingsSection title={t("settings.tab.cowork")}>

        {/* ── 浏览器自动化 ── */}
        <OptionalModule title={t("cowork.browser")} description={t("cowork.browserModDesc")} enabled={browserOn} onToggle={toggleBrowser}>
          <div className="optional-module__controls">
            <div className="set-input-browse">
              <input
                className="mem-input set-grow"
                placeholder={t("cowork.browserPath")}
                value={draft.browserPath}
                onChange={e => setDraft({ ...draft, browserPath: e.target.value })}
                onBlur={commitCurrent}
                onKeyDown={e => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); }}
              />
              <button
                type="button"
                className="btn btn--small set-input-browse__btn"
                disabled={busy || browserDetecting}
                onClick={checkBrowser}
              >
                {browserDetecting ? <Loader2 className="spinner" size={14} /> : t("cowork.browserDetect")}
              </button>
              {draft.detectedBrowser && (
                <span className="mem-hint" style={{ margin: 0, whiteSpace: "nowrap" }}>
                  {t("cowork.browserDetected", { name: draft.detectedBrowser })}
                </span>
              )}
            </div>
          </div>
        </OptionalModule>

        {/* ── PPT 生成 ── */}
        <OptionalModule title={t("cowork.ppt")} description="通过 SVG 路径生成专业 PPT，支持模板、多种布局、质量检查" enabled={pptOn} onToggle={togglePpt}>
          <div className="optional-module__controls">
            <div className="set-input-browse">
              <select
                className="mem-input set-grow"
                value={draft.pptActiveTemplate}
                onChange={e => setDraft(d => { const n = { ...d, pptActiveTemplate: e.target.value }; commitDraft(n); return n; })}
              >
                <option value="">— 选择模板 —</option>
                {(draft.pptTemplates ?? []).map(tpl => (
                  <option key={tpl.id} value={tpl.id}>{tpl.name}</option>
                ))}
              </select>
              <button
                type="button"
                className="btn btn--small set-input-browse__btn"
                disabled={busy}
                onClick={() => void openTemplateDir()}
              >
                打开模板目录
              </button>
            </div>
            <div style={{ marginTop: "12px" }}>
              <label style={{ fontSize: "12px", color: "#666", marginBottom: "4px", display: "block" }}>生成模式</label>
              <select
                className="mem-input"
                value={draft.pptMode || "fast"}
                onChange={e => setDraft(d => { const n = { ...d, pptMode: e.target.value }; commitDraft(n); return n; })}
                style={{ width: "100%" }}
              >
                <option value="fast">快速模式（一次生成，不返工）</option>
                <option value="validate">校验模式（生成后检查，有问题返工）</option>
              </select>
            </div>
          </div>
        </OptionalModule>

        {/* ── 邮箱（多账户）── */}
        <OptionalModule
          title={t("cowork.mail")}
          description={t("cowork.mailModDesc")}
          enabled={mailOn}
          onToggle={toggleMail}
          statusDot={mailOn ? (
            <span
              className={"mail-status-dot mail-status-dot--" + (mailStatus["__default__"] ?? "idle")}
              title={(mailStatus["__default__"] === "error" ? mailMessage["__default__"] : (mailStatus["__default__"] === "ok" ? "连接正常" : "")) || ""}
            />
          ) : undefined}
        >
          <div className="optional-module__controls">
            {/* Onboarding hint: most users don't know what an "auth code" is or
                that the server is auto-filled. This banner sits above the
                account list so first-time setup is self-explanatory. */}
            <div className="cowork-mail-guide">
              <div className="cowork-mail-guide__step">
                <strong>{t("cowork.mailGuideStep1Title")}</strong>
                <span>{t("cowork.mailGuideStep1")}</span>
              </div>
              <div className="cowork-mail-guide__step">
                <strong>{t("cowork.mailGuideStep2Title")}</strong>
                <span>{t("cowork.mailGuideStep2")}</span>
              </div>
              <div className="cowork-mail-guide__step">
                <strong>{t("cowork.mailGuideStep3Title")}</strong>
                <span>{t("cowork.mailGuideStep3")}</span>
              </div>
            </div>
            {(draft.emailAccounts ?? []).length === 0 ? (
              <div className="mem-hint cowork-mail-empty">
                {t("cowork.mailEmpty")}
              </div>
            ) : (
              <div className="cowork-mail-accounts">
                {(draft.emailAccounts ?? []).map((acct, i) => {
                  // Status is keyed by index, not account name: the user can
                  // rename an account, and a name-based key would lose the
                  // status dot the moment they edit the name field.
                  const skey = String(i);
                  const st = mailStatus[skey] ?? "idle";
                  const probing = st === "checking";
                  return (
                    <div className="cowork-mail-account" key={i}>
                      {/* Single-row layout: account name + email + auth code +
                          default radio + delete, all on one line so the whole
                          account fits at a glance. The test-connection row sits
                          below it. */}
                      <div className="cowork-mail-account__row">
                        <label className="cowork-mail-account__field">
                          <span className="cowork-mail-account__field-label">{t("cowork.mailAccountName")}</span>
                          <input
                            className="mem-input cowork-mail-account__name"
                            placeholder={t("cowork.mailAccountNameHint")}
                            value={acct.name}
                            onChange={e => patchAccount(i, { name: e.target.value })}
                            onBlur={() => void commitMailThenProbe(i, acct.name)}
                            onKeyDown={e => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); }}
                          />
                        </label>
                        <label className="cowork-mail-account__field cowork-mail-account__field--grow">
                          <span className="cowork-mail-account__field-label">{t("cowork.mailAccount")}</span>
                          <input
                            className="mem-input"
                            placeholder={t("cowork.mailAccountHint")}
                            value={acct.smtp.username || ""}
                            onChange={e => {
                            const email = e.target.value;
                            // Auto-infer SMTP/IMAP servers from the email domain
                            // (imap.<domain> / smtp.<domain>) ONLY when the user
                            // hasn't manually set them. This handles the China
                            // Mobile family (50+ subdomains like bj.chinamobile.com,
                            // cmtt.chinamobile.com) and most standard providers,
                            // without forcing the user to open Advanced settings.
                            const domain = email.split("@")[1]?.trim().toLowerCase() ?? "";
                            const smtpHost = acct.smtp.host || (domain ? `smtp.${domain}` : "");
                            const imapHost = acct.imap.host || (domain ? `imap.${domain}` : "");
                            patchAccount(i, {
                              smtp: {
                                ...acct.smtp,
                                username: email,
                                from: email,
                                host: smtpHost,
                                // Default to implicit TLS (465/993) — the common
                                // case for chinamobile.com and 139.com. The user
                                // can override in Advanced if their server uses
                                // STARTTLS (587) or plain (25).
                                port: acct.smtp.port || (domain ? 465 : acct.smtp.port),
                                encryptionMode: acct.smtp.encryptionMode || (domain ? "tls" : acct.smtp.encryptionMode),
                                useTLS: acct.smtp.encryptionMode ? acct.smtp.useTLS : (domain ? true : acct.smtp.useTLS),
                              },
                              imap: {
                                ...acct.imap,
                                username: email,
                                host: imapHost,
                                port: acct.imap.port || (domain ? 993 : acct.imap.port),
                              },
                            });
                          }}
                          onBlur={() => void commitMailThenProbe(i, acct.name)}
                          onKeyDown={e => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); }}
                        />
                        </label>
                        <label className="cowork-mail-account__field cowork-mail-account__field--grow">
                          <span className="cowork-mail-account__field-label">{t("cowork.mailAuthCode")}</span>
                          <input
                            className="mem-input"
                            type="password"
                            placeholder={acct.passwordSet ? t("cowork.secretSet") : t("cowork.mailAuthCodeHint")}
                            value={acct.password}
                            onChange={e => patchAccount(i, { password: e.target.value })}
                            onBlur={() => void commitMailThenProbe(i, acct.name)}
                            onKeyDown={e => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); }}
                          />
                        </label>
                        <div className="cowork-mail-account__actions">
                          <label className="cowork-mail-account__default" title={t("cowork.mailDefault")}>
                          <input
                            type="radio"
                            name="mail-default"
                            checked={!!acct.default}
                            onChange={() => setAccountDefault(i)}
                          />
                          {t("cowork.mailDefault")}
                        </label>
                        <button
                          className="cowork-task-card__btn cowork-task-card__btn--danger"
                          title={t("cowork.mailDelete")}
                          onClick={() => removeAccount(i)}
                        >
                          <Trash2 size={14} />
                        </button>
                        </div>
                      </div>
                      {/* Advanced server settings (collapsible). Only the server
                          ADDRESSES are exposed — ports (465/993) and encryption
                          (SSL/TLS) are fixed defaults that work for 139,
                          chinamobile, and most providers, so they're not shown
                          to avoid clutter. The user only overrides the host when
                          the auto-inferred smtp./imap.<domain> is wrong. */}
                      <details className="cowork-mail-account__advanced">
                        <summary>{t("cowork.mailAdvanced")}</summary>
                        <div className="cowork-mail-account__server-row">
                          <label className="cowork-mail-account__server-label">
                            <span>SMTP {t("cowork.mailServer")} <em className="cowork-mail-account__server-ex">{t("cowork.mailServerExSMTP")}</em></span>
                            <input
                              className="mem-input"
                              placeholder="smtp.example.com"
                              value={acct.smtp.host}
                              onChange={e => patchAccount(i, { smtp: { ...acct.smtp, host: e.target.value } })}
                              onBlur={() => void commitMailThenProbe(i, acct.name)}
                              onKeyDown={e => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); }}
                            />
                          </label>
                          <label className="cowork-mail-account__server-label">
                            <span>IMAP {t("cowork.mailServer")} <em className="cowork-mail-account__server-ex">{t("cowork.mailServerExIMAP")}</em></span>
                            <input
                              className="mem-input"
                              placeholder="imap.example.com"
                              value={acct.imap.host}
                              onChange={e => patchAccount(i, { imap: { ...acct.imap, host: e.target.value } })}
                              onBlur={() => void commitMailThenProbe(i, acct.name)}
                              onKeyDown={e => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); }}
                            />
                          </label>
                        </div>
                        <span className="cowork-mail-account__server-note">
                          {t("cowork.mailServerNote")}
                        </span>
                      </details>
                      {/* Test-connection row: an explicit button so the user can
                          verify connectivity on demand (not only after a save),
                          plus a status label next to the dot so ok/error is
                          readable without hovering. */}
                      <div className="cowork-mail-account__test">
                        <button
                          className="btn btn--small cowork-mail-account__test-btn"
                          onClick={() => void commitMailThenProbe(i, acct.name)}
                          disabled={probing || busy}
                          title={t("cowork.mailTest")}
                        >
                          {probing ? <Loader2 className="spinner" size={13} /> : <RefreshCw size={13} />}
                          {t("cowork.mailTest")}
                        </button>
                        <span
                          className={"mail-status-dot mail-status-dot--" + st}
                          title={st === "error" ? (mailMessage[skey] || "") : (st === "ok" ? (mailMessage[skey] || "连接正常") : "")}
                        />
                        <span className={"cowork-mail-account__status-label cowork-mail-account__status-label--" + st}>
                          {st === "ok" ? t("cowork.mailStatusOk")
                           : st === "error" ? t("cowork.mailStatusError")
                           : st === "checking" ? t("cowork.mailStatusChecking")
                           : ""}
                        </span>
                        {st === "error" && mailMessage[skey] && (
                          <span className="cowork-mail-account__status-msg" title={mailMessage[skey]}>{mailMessage[skey]}</span>
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
            <button
              className="btn btn--small cowork-mail-add"
              onClick={newAccount}
              disabled={busy}
            >
              {t("cowork.mailAdd")}
            </button>
            {/* Allow scheduled tasks to send email without interactive approval.
                Headless mode (no tab) otherwise denies email_send as an
                irreversible outward op. Surfaced as a checkbox so the user
                explicitly opts in, rather than silently defaulting. */}
            <label className="cowork-mail-autosend" title={t("cowork.mailAutoSendHint")}>
              <input
                type="checkbox"
                checked={!!draft.allowHeadlessEmail}
                onChange={e => setDraft(d => {
                  const n = { ...d, allowHeadlessEmail: e.target.checked };
                  dirtyRef.current = true;
                  commitDraft(n);
                  return n;
                })}
              />
              <span>{t("cowork.mailAutoSend")}</span>
            </label>
          </div>
        </OptionalModule>

        {/* ── RAG 语义检索 ── */}
        <OptionalModule title={t("cowork.rag")} description={t("cowork.ragModDesc")} enabled={ragOn} onToggle={toggleRag}>
          <div className="optional-module__controls">
            <span className="mem-hint">{t("cowork.ragHint")}</span>
            <input
              className="mem-input set-grow"
              placeholder={t("cowork.ragModel")}
              value={draft.embeddingModel}
              onChange={e => setDraft({ ...draft, embeddingModel: e.target.value })}
              onBlur={commitCurrent}
              onKeyDown={e => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); }}
            />
          </div>
        </OptionalModule>

      </SettingsSection>

      {/* --- 快捷截屏（全局热键 → VLM 识别 → IM 回复） --- */}
      <SettingsSection title={t("settings.screenshotTitle")}>
        <SettingsField label={t("settings.screenshotEnableLabel")} hint={t("settings.screenshotEnableHint")}>
          <label className="cap-switch">
            <input
              type="checkbox"
              checked={draft.screenshotEnabled ?? false}
              onChange={e => setDraft(d => { const n = { ...d, screenshotEnabled: e.target.checked }; commitDraft(n); return n; })}
            />
            <span className="cap-switch__track" />
          </label>
        </SettingsField>
        <SettingsField label={t("settings.screenshotHotkeyLabel")} hint={t("settings.screenshotHotkeyHint")}>
          <div className="set-input-browse">
            <input
              className="mem-input"
              value={recordingHotkey ? t("settings.hotkeyRecord") : (draft.screenshotHotkey ?? "Ctrl+Shift+S")}
              readOnly={recordingHotkey}
              placeholder="Ctrl+Shift+S"
              onKeyDown={(e) => {
                if (!recordingHotkey) return;
                const modKeys = ["Control", "Alt", "Shift", "Meta"];
                if (modKeys.includes(e.key)) return;
                e.preventDefault();
                const parts: string[] = [];
                if (e.ctrlKey) parts.push("Ctrl");
                if (e.altKey) parts.push("Alt");
                if (e.shiftKey) parts.push("Shift");
                if (e.metaKey) parts.push(e.metaKey ? "Win" : "");
                const key = e.key.length === 1 ? e.key.toUpperCase() : e.key;
                parts.push(key);
                setDraft(d => { const n = { ...d, screenshotHotkey: parts.filter(Boolean).join("+") }; commitDraft(n); return n; });
                setRecordingHotkey(false);
              }}
              onBlur={() => { if (recordingHotkey) setRecordingHotkey(false); else commitCurrent(); }}
              onChange={e => !recordingHotkey && setDraft({ ...draft, screenshotHotkey: e.target.value })}
            />
            <button
              type="button"
              className={"btn btn--small set-input-browse__btn" + (recordingHotkey ? " btn--primary" : "")}
              onClick={() => setRecordingHotkey(!recordingHotkey)}
            >
              {recordingHotkey ? "Esc" : t("settings.hotkeyRecordBtn")}
            </button>
          </div>
        </SettingsField>
      </SettingsSection>

      {/* --- 紧急停止（全局热键 → 中断 AI 桌面自动化） --- */}
      <SettingsSection title={t("settings.estopTitle")}>
        <SettingsField label={t("settings.estopHotkeyLabel")} hint={t("settings.estopHotkeyHint")}>
          <div className="set-input-browse">
            <input
              className="mem-input"
              value={recordingEStopHotkey ? t("settings.hotkeyRecord") : (draft.estopHotkey ?? "Ctrl+Shift+Pause")}
              readOnly={recordingEStopHotkey}
              placeholder="Ctrl+Shift+Pause"
              onKeyDown={(e) => {
                if (!recordingEStopHotkey) return;
                const modKeys = ["Control", "Alt", "Shift", "Meta"];
                if (modKeys.includes(e.key)) return;
                e.preventDefault();
                const parts: string[] = [];
                if (e.ctrlKey) parts.push("Ctrl");
                if (e.altKey) parts.push("Alt");
                if (e.shiftKey) parts.push("Shift");
                if (e.metaKey) parts.push(e.metaKey ? "Win" : "");
                const key = e.key.length === 1 ? e.key.toUpperCase() : e.key;
                parts.push(key);
                setDraft(d => { const n = { ...d, estopHotkey: parts.filter(Boolean).join("+") }; commitDraft(n); return n; });
                setRecordingEStopHotkey(false);
              }}
              onBlur={() => { if (recordingEStopHotkey) setRecordingEStopHotkey(false); else commitCurrent(); }}
              onChange={e => !recordingEStopHotkey && setDraft({ ...draft, estopHotkey: e.target.value })}
            />
            <button
              type="button"
              className={"btn btn--small set-input-browse__btn" + (recordingEStopHotkey ? " btn--primary" : "")}
              onClick={() => setRecordingEStopHotkey(!recordingEStopHotkey)}
            >
              {recordingEStopHotkey ? "Esc" : t("settings.hotkeyRecordBtn")}
            </button>
          </div>
        </SettingsField>
      </SettingsSection>
    </>
  );
}


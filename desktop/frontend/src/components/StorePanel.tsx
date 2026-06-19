import { useCallback, useEffect, useRef, useState } from "react";
import { Loader2, Search, Star, Download, ExternalLink, Trash2, RefreshCw, Shield, ShieldAlert, X, Store, Globe, Plus } from "lucide-react";
import { app } from "../lib/bridge";
import { useToast } from "../lib/toast";
import { useT } from "../lib/i18n";
import type { StoreSkillView, StoreSkillDetailView, StoreUpdateView, StoreStatusView } from "../lib/types";

type SortKey = "recommended" | "stars" | "installs" | "updated" | "newest";

const SORT_KEYS: SortKey[] = ["recommended", "stars", "installs", "updated", "newest"];
const PRESET_SOURCES: Record<string, string> = {
  clawhub: "https://clawhub.ai/api/v1",
};

export function StorePanel() {
  const t = useT();
  const { showToast } = useToast();
  const [status, setStatus] = useState<StoreStatusView | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    app.StoreStatus().then(setStatus).catch(() => setStatus({ enabled: false, source: "clawhub", baseUrl: "" } as StoreStatusView)).finally(() => setLoading(false));
  }, []);

  if (loading) {
    return <div className="store__loading"><Loader2 className="app-spin" size={20} /><span>{t("store.loading")}</span></div>;
  }

  if (!status?.enabled) {
    return <StoreSetup status={status} onEnabled={() => app.StoreStatus().then(setStatus)} showToast={showToast} />;
  }

  return <StoreBrowse showToast={showToast} />;
}

/* ── Setup screen (store disabled) ──────────────────────────────────────── */

function StoreSetup({ onEnabled, showToast }: {
  status?: StoreStatusView | null;
  onEnabled: () => void;
  showToast: (msg: string, level?: "info" | "warn") => void;
}) {
  const t = useT();
  const [enabling, setEnabling] = useState(false);
  const [customUrl, setCustomUrl] = useState("");
  const [showCustom, setShowCustom] = useState(false);

  const enableClawHub = async () => {
    setEnabling(true);
    try {
      await app.SetStoreSourceURL("clawhub", PRESET_SOURCES.clawhub);
      onEnabled();
      showToast(t("store.setup.connected"), "info");
    } catch (e) {
      showToast(t("store.error.connectFailed"), "warn");
    } finally {
      setEnabling(false);
    }
  };

  const enableCustom = async () => {
    const url = customUrl.trim();
    if (!url) return;
    setEnabling(true);
    try {
      await app.SetStoreSourceURL("custom", url);
      onEnabled();
      showToast(t("store.setup.connected"), "info");
    } catch (e) {
      showToast(t("store.error.connectFailed"), "warn");
    } finally {
      setEnabling(false);
    }
  };

  return (
    <div className="store-setup">
      <div className="store-setup__card">
        <Store size={32} className="store-setup__icon" />
        <h2>{t("store.setup.title")}</h2>
        <p className="store-setup__desc">{t("store.setup.desc")}</p>

        <div className="store-setup__options">
          <button type="button" className="btn btn--primary store-setup__btn" disabled={enabling} onClick={() => void enableClawHub()}>
            {enabling ? <Loader2 className="app-spin" size={14} /> : <Globe size={14} />}
            {t("store.setup.connectClawHub")}
          </button>

          <button type="button" className="btn btn--secondary store-setup__btn" onClick={() => setShowCustom((v) => !v)}>
            <Plus size={14} />
            {t("store.setup.addCustom")}
          </button>
        </div>

        {showCustom && (
          <div className="store-setup__custom">
            <input
              type="text"
              value={customUrl}
              onChange={(e) => setCustomUrl(e.target.value)}
              placeholder={t("store.setup.urlPlaceholder")}
              className="store-setup__input"
            />
            <button type="button" className="btn btn--primary btn--small" disabled={enabling || !customUrl.trim()} onClick={() => void enableCustom()}>
              {t("store.setup.connect")}
            </button>
          </div>
        )}

        <p className="store-setup__note">{t("store.setup.note")}</p>
      </div>
    </div>
  );
}

/* ── Browse screen (store enabled) ──────────────────────────────────────── */

function StoreBrowse({ showToast }: { showToast: (msg: string, level?: "info" | "warn") => void }) {
  const t = useT();
  const [items, setItems] = useState<StoreSkillView[]>([]);
  const [loading, setLoading] = useState(false);
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<SortKey>("recommended");
  const [cursor, setCursor] = useState("");
  const [hasMore, setHasMore] = useState(false);
  const [detail, setDetail] = useState<StoreSkillDetailView | null>(null);
  const [updates, setUpdates] = useState<StoreUpdateView[]>([]);
  const [installing, setInstalling] = useState<Set<string>>(new Set());
  const searchTimer = useRef(0);

  const loadList = useCallback(async (s: SortKey, c: string, append: boolean) => {
    setLoading(true);
    try {
      const result = await app.StoreSkills(s, c, 20);
      setItems((prev) => append ? [...prev, ...result.items] : result.items);
      setCursor(result.nextCursor || "");
      setHasMore(!!result.nextCursor);
    } catch (e) {
      console.warn("store list failed", e);
      showToast(t("store.error.loadFailed"), "warn");
    } finally {
      setLoading(false);
    }
  }, [showToast, t]);

  const doSearch = useCallback(async (q: string) => {
    if (!q.trim()) {
      loadList(sort, "", false);
      return;
    }
    setLoading(true);
    try {
      const results = await app.StoreSearch(q.trim(), 30);
      setItems(results);
      setCursor("");
      setHasMore(false);
    } catch (e) {
      console.warn("store search failed", e);
      showToast(t("store.error.loadFailed"), "warn");
    } finally {
      setLoading(false);
    }
  }, [loadList, sort, showToast, t]);

  const loadUpdates = useCallback(async () => {
    try {
      const u = await app.StoreCheckUpdates();
      setUpdates(u);
    } catch { /* ignore */ }
  }, []);

  useEffect(() => { loadList(sort, "", false); }, [sort, loadList]);
  useEffect(() => { loadUpdates(); }, [loadUpdates]);

  const onSearchChange = (value: string) => {
    setQuery(value);
    window.clearTimeout(searchTimer.current);
    searchTimer.current = window.setTimeout(() => doSearch(value), 350);
  };

  const clearSearch = () => {
    setQuery("");
    window.clearTimeout(searchTimer.current);
    loadList(sort, "", false);
  };

  const onSortChange = (s: SortKey) => {
    setSort(s);
    setQuery("");
  };

  const openDetail = async (slug: string) => {
    try {
      const d = await app.StoreSkillDetail(slug);
      setDetail(d);
    } catch (e) {
      console.warn("store detail failed", e);
      showToast(t("store.error.loadFailed"), "warn");
    }
  };

  const doInstall = async (slug: string, version: string) => {
    setInstalling((prev) => new Set(prev).add(slug));
    try {
      await app.StoreInstallSkill(slug, version);
      await app.RefreshSkills();
      showToast(t("store.toast.installed", { name: slug }), "info");
      if (query.trim()) {
        await doSearch(query);
      } else {
        await loadList(sort, "", false);
      }
      await loadUpdates();
    } catch (e) {
      console.warn("store install failed", e);
      showToast(t("store.error.installFailed"), "warn");
    } finally {
      setInstalling((prev) => { const n = new Set(prev); n.delete(slug); return n; });
    }
  };

  const doUninstall = async (slug: string) => {
    try {
      await app.StoreUninstallSkill(slug);
      await app.RefreshSkills();
      showToast(t("store.toast.uninstalled", { name: slug }), "info");
      if (detail?.slug === slug) setDetail(null);
      if (query.trim()) {
        await doSearch(query);
      } else {
        await loadList(sort, "", false);
      }
      await loadUpdates();
    } catch (e) {
      console.warn("store uninstall failed", e);
      showToast(t("store.error.uninstallFailed"), "warn");
    }
  };

  return (
    <div className="store">
      <div className="store__toolbar">
        <div className="store__search">
          <Search size={14} aria-hidden="true" />
          <input
            type="text"
            value={query}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder={t("store.search")}
            className="store__search-input"
          />
          {query && (
            <button type="button" className="store__search-clear" onClick={clearSearch} aria-label={t("store.clearSearch")}>
              <X size={13} />
            </button>
          )}
        </div>
        <div className="store__sort">
          {SORT_KEYS.map((k) => (
            <button
              key={k}
              type="button"
              className={`store__sort-btn${sort === k ? " store__sort-btn--active" : ""}`}
              onClick={() => onSortChange(k)}
            >
              {t(`store.sort.${k}`)}
            </button>
          ))}
        </div>
      </div>

      {updates.length > 0 && (
        <div className="store__updates">
          <RefreshCw size={14} />
          <span>{t("store.updatesAvailable", { n: updates.length })}</span>
          <div className="store__updates-list">
            {updates.map((u) => (
              <button key={u.slug} type="button" className="btn btn--primary btn--small" onClick={() => void doInstall(u.slug, u.latestVersion)}>
                {u.name} → {u.latestVersion}
              </button>
            ))}
          </div>
        </div>
      )}

      {loading && items.length === 0 ? (
        <div className="store__loading"><Loader2 className="app-spin" size={20} /><span>{t("store.loading")}</span></div>
      ) : items.length === 0 ? (
        <div className="store__empty">{t("store.noResults")}</div>
      ) : (
        <div className="store__grid">
          {items.map((item) => (
            <StoreCard
              key={item.slug}
              item={item}
              installing={installing.has(item.slug)}
              onInstall={() => void doInstall(item.slug, item.latestVersion)}
              onUninstall={() => {
                if (window.confirm(t("store.confirmUninstall", { name: item.displayName }))) {
                  void doUninstall(item.slug);
                }
              }}
              onDetail={() => void openDetail(item.slug)}
            />
          ))}
        </div>
      )}

      {hasMore && !query.trim() && (
        <div className="store__more">
          <button type="button" className="btn btn--secondary" disabled={loading} onClick={() => void loadList(sort, cursor, true)}>
            {loading ? <Loader2 className="app-spin" size={14} /> : null}
            {t("store.loadMore")}
          </button>
        </div>
      )}

      {detail && (
        <StoreDetailDrawer
          detail={detail}
          onClose={() => setDetail(null)}
          onInstall={(v) => void doInstall(detail.slug, v)}
          onUninstall={() => {
            if (window.confirm(t("store.confirmUninstall", { name: detail.displayName }))) {
              void doUninstall(detail.slug);
            }
          }}
          installing={installing.has(detail.slug)}
        />
      )}
    </div>
  );
}

function StoreCard({ item, installing, onInstall, onUninstall, onDetail }: {
  item: StoreSkillView;
  installing: boolean;
  onInstall: () => void;
  onUninstall: () => void;
  onDetail: () => void;
}) {
  const t = useT();
  const needsUpdate = item.installed && item.installedVersion && item.latestVersion && item.installedVersion !== item.latestVersion;
  return (
    <div className="store-card">
      <div className="store-card__head">
        <h3 className="store-card__name" title={item.displayName}>{item.displayName}</h3>
        {item.latestVersion && <span className="store-card__version">v{item.latestVersion}</span>}
      </div>
      <p className="store-card__summary" title={item.summary}>{item.summary}</p>
      <div className="store-card__meta">
        <span className="store-card__stat"><Star size={12} /> {item.stars}</span>
        <span className="store-card__stat"><Download size={12} /> {formatCount(item.installs)}</span>
      </div>
      <div className="store-card__actions">
        {item.installed ? (
          <>
            <button type="button" className={`btn btn--small ${needsUpdate ? "btn--primary" : "btn--secondary"}`} disabled={!needsUpdate} onClick={onInstall}>
              {needsUpdate ? <><RefreshCw size={12} /> {t("store.update")}</> : <>{t("store.installed")}</>}
            </button>
            <button type="button" className="btn btn--secondary btn--small" onClick={onUninstall} title={t("store.uninstall")}>
              <Trash2 size={12} />
            </button>
          </>
        ) : (
          <button type="button" className="btn btn--primary btn--small" disabled={installing} onClick={onInstall}>
            {installing ? <Loader2 className="app-spin" size={12} /> : <Download size={12} />}
            {t("store.install")}
          </button>
        )}
        <button type="button" className="btn btn--secondary btn--small" onClick={onDetail}>
          {t("store.detail")}
        </button>
      </div>
    </div>
  );
}

function StoreDetailDrawer({ detail, onClose, onInstall, onUninstall, installing }: {
  detail: StoreSkillDetailView;
  onClose: () => void;
  onInstall: (version: string) => void;
  onUninstall: () => void;
  installing: boolean;
}) {
  const t = useT();
  const needsUpdate = detail.installed && detail.installedVersion && detail.latestVersion && detail.installedVersion !== detail.latestVersion;
  return (
    <div className="store-detail-overlay" onClick={onClose}>
      <div className="store-detail" onClick={(e) => e.stopPropagation()}>
        <div className="store-detail__head">
          <div>
            <h2>{detail.displayName}</h2>
            {detail.ownerHandle && (
              <span className="store-detail__owner">{t("store.by")} {detail.ownerName || detail.ownerHandle}</span>
            )}
          </div>
          <button type="button" className="btn btn--secondary btn--small" onClick={onClose}>✕</button>
        </div>

        {detail.isSuspicious && (
          <div className="store-detail__warning">
            <ShieldAlert size={14} /> {t("store.security.suspicious")}
          </div>
        )}
        {!detail.isSuspicious && detail.verdict && (
          <div className="store-detail__safe">
            <Shield size={14} /> {t("store.security.clean")}
          </div>
        )}

        <div className="store-detail__stats">
          <span><Star size={14} /> {detail.stars} {t("store.stars")}</span>
          <span><Download size={14} /> {formatCount(detail.installs)} {t("store.installs")}</span>
          {detail.latestVersion && <span>{t("store.version")}: v{detail.latestVersion}</span>}
        </div>

        <div className="store-detail__summary">{detail.summary}</div>

        {detail.changelog && (
          <div className="store-detail__section">
            <h4>{t("store.changelog")}</h4>
            <pre className="store-detail__changelog">{detail.changelog}</pre>
          </div>
        )}

        <div className="store-detail__actions">
          {detail.installed ? (
            <>
              <button type="button" className="btn btn--secondary" disabled>
                {t("store.installed")} v{detail.installedVersion}
              </button>
              {needsUpdate && (
                <button type="button" className="btn btn--primary" disabled={installing} onClick={() => onInstall(detail.latestVersion)}>
                  {installing ? <Loader2 className="app-spin" size={14} /> : <RefreshCw size={14} />}
                  {t("store.update")} → v{detail.latestVersion}
                </button>
              )}
              <button type="button" className="btn btn--secondary" onClick={onUninstall}>
                <Trash2 size={14} /> {t("store.uninstall")}
              </button>
            </>
          ) : (
            <button type="button" className="btn btn--primary" disabled={installing} onClick={() => onInstall(detail.latestVersion)}>
              {installing ? <Loader2 className="app-spin" size={14} /> : <Download size={14} />}
              {t("store.install")} v{detail.latestVersion}
            </button>
          )}
          <a href={`https://clawhub.ai/${detail.ownerHandle}/${detail.slug}`} target="_blank" rel="noopener" className="btn btn--secondary">
            <ExternalLink size={14} /> ClawHub
          </a>
        </div>
      </div>
    </div>
  );
}

function formatCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

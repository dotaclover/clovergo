import type { ViewName } from "./context";
import type { MessageKey } from "./i18n";
import type { PlatformID } from "./types";

/**
 * The platforms the product ships, in menu order. This mirrors
 * internal/platforms/registry.go: platforms are code, so the navigation can be
 * static and a removed platform disappears from the menu at the same moment it
 * stops being generatable.
 *
 * There is one entry today — CloverGo writes WeChat Official Account articles
 * and nothing else.
 */
export interface PlatformNav {
  id: PlatformID;
  /** i18n key of the menu group label; render with t(). */
  labelKey: MessageKey;
  config: ViewName;
  /** 草稿：填灵感、选来源，生成一篇文章。 */
  compose: ViewName;
  /** 列表：历史草稿。 */
  list: ViewName;
  /** 详情：从列表进入的一篇草稿，可查看、修改、删除、推送。 */
  detail: ViewName;
}

export const PLATFORM_NAV: PlatformNav[] = [
  {
    id: "gongzhonghao",
    labelKey: "platform.gongzhonghao",
    config: "gzh-config",
    compose: "gzh-compose",
    list: "gzh-list",
    detail: "gzh-draft",
  },
];

/** The platform used when a screen needs one and none is chosen. */
export const DEFAULT_PLATFORM: PlatformNav = PLATFORM_NAV[0];

const SCOPE = new Map<ViewName, PlatformNav>();
for (const entry of PLATFORM_NAV) {
  SCOPE.set(entry.config, entry);
  SCOPE.set(entry.compose, entry);
  SCOPE.set(entry.list, entry);
  SCOPE.set(entry.detail, entry);
}

/** The platform a platform-scoped view belongs to, or undefined for a shared view. */
export function platformOf(view: ViewName): PlatformNav | undefined {
  return SCOPE.get(view);
}

/** The 配置 screen of a platform, used by the "去编辑" link on prompt layers. */
export function configViewFor(platformID: string): ViewName | undefined {
  return PLATFORM_NAV.find((entry) => entry.id === platformID)?.config;
}

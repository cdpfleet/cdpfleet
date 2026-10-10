import type { Browser, BrowserType, ConnectOptions, ConnectOverCDPOptions } from 'playwright-core';

export interface CdpfleetOptions {
  /** Defaults to the CDPFLEET_API_KEY environment variable. */
  apiKey?: string;
  starterUrl?: string;
  apiUrl?: string;
  /** Seconds `launch` keeps retrying 503/429 (default 120). */
  maxWait?: number;
}

/** Launch options: passed to the API unchanged. See https://cdpfleet.com/docs/builder */
export interface LaunchOptions {
  proxy: string | string[] | { server: string; username?: string; password?: string };
  headless?: boolean | 'new' | 'shell' | 'virtual';
  proxy_rules?: Array<{ name?: string; hosts: string[]; proxy: string | string[] }>;
  proxy_updatable?: boolean;
  inactivity_timeout?: number | string;
  overall_timeout?: number | string;
  channel?: 'stable' | 'beta' | 'dev' | 'developer';
  version?: string;
  cdp?: boolean;
  keep_alive?: boolean;
  record?: boolean;
  live_view?: boolean;
  screen_size?: string | { width: number; height: number };
  [option: string]: unknown;
}

export class CdpfleetError extends Error {
  status: number;
  code: string | null;
  body: unknown;
}

export class Session {
  readonly id: string;
  readonly wsUrl: string;
  readonly cdpUrl: string | null;
  readonly browserName: string;
  readonly resourceClass: string;
  readonly weight: number;
  readonly expiresAt: string;
  readonly keepAlive: boolean;
  readonly raw: Record<string, unknown>;
  connect(browserType: BrowserType, options?: ConnectOptions): Promise<Browser>;
  connectOverCDP(browserType: BrowserType, options?: ConnectOverCDPOptions): Promise<Browser>;
  liveLink(mode?: 'view' | 'control'): Promise<{ live_url: string; protocol: 'vnc' | 'screencast'; mode: string; expires_in_s: number; [k: string]: unknown }>;
  swapProxy(proxy: LaunchOptions['proxy'], proxyRules?: LaunchOptions['proxy_rules']): Promise<unknown>;
  end(): Promise<void>;
  close(): Promise<void>;
  [Symbol.asyncDispose](): Promise<void>;
}

export class Cdpfleet {
  constructor(options?: CdpfleetOptions);
  launch(browser: string, options: LaunchOptions): Promise<Session>;
  me(): Promise<any>;
  usage(): Promise<any>;
  threads(): Promise<any>;
  sessions(query?: { days?: number; limit?: number }): Promise<any>;
  recordings(): Promise<any>;
  recordingUrl(id: string): Promise<string>;
  deleteRecording(id: string): Promise<any>;
  proxyBalance(): Promise<any>;
  endSession(sessionId: string): Promise<void>;
}

export default Cdpfleet;

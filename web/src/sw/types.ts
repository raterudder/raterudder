/**
 * RateRudder Push Notification payload formats and Service Worker interfaces.
 */

export interface PushPayloadData {
  url?: string;
  id?: string;
  logID?: string;
  ts?: number;
  metadata?: Record<string, unknown>;
  [key: string]: unknown;
}

export interface PushPayload {
  title?: string;
  body?: string;
  tag?: string;
  icon?: string;
  badge?: string;
  url?: string;
  id?: string;
  data?: PushPayloadData;
}

export interface PushOptions extends NotificationOptions {
  data: PushPayloadData;
  renotify?: boolean;
}

declare global {
  interface NotificationOptions {
    renotify?: boolean;
  }
}

export interface NotificationClickData {
  url?: string;
  id?: string;
  logID?: string;
  ts?: number;
  [key: string]: unknown;
}

export interface NotificationErrorInfo {
  message: string;
  name?: string;
  stack?: string;
  phase: 'payload_parsing' | 'primary_show_notification' | 'fallback_show_notification';
  title?: string;
  tag?: string;
  data?: Record<string, unknown>;
}

export interface BrowserReportPayload {
  type: string;
  age: number;
  url: string;
  user_agent: string;
  body: unknown;
}

export interface ExtendableEvent extends Event {
  waitUntil(promise: Promise<unknown>): void;
}

export interface PushMessageData {
  arrayBuffer(): ArrayBuffer;
  blob(): Blob;
  json(): any;
  text(): string;
}

export interface PushEvent extends ExtendableEvent {
  readonly data: PushMessageData | null;
}

export interface NotificationEvent extends ExtendableEvent {
  readonly notification: Notification;
  readonly action?: string;
}

export interface PushSubscriptionChangeEvent extends ExtendableEvent {
  readonly newSubscription?: PushSubscription | null;
  readonly oldSubscription?: PushSubscription | null;
}

export interface WindowClient {
  readonly id: string;
  readonly url: string;
  readonly focused?: boolean;
  focus(): Promise<WindowClient>;
  navigate(url: string): Promise<WindowClient | null>;
}

export interface Clients {
  claim(): Promise<void>;
  get(id: string): Promise<WindowClient | undefined>;
  matchAll(options?: {
    type?: 'window' | 'worker' | 'sharedworker' | 'all';
    includeUncontrolled?: boolean;
  }): Promise<readonly WindowClient[]>;
  openWindow(url: string): Promise<WindowClient | null>;
}

export interface ServiceWorkerGlobalScope {
  readonly registration: ServiceWorkerRegistration;
  readonly clients: Clients;
  readonly location: Location;
  skipWaiting(): Promise<void>;
  addEventListener(type: string, listener: (event: any) => void): void;
}

import { registerServiceWorker } from './handlers';
import type { ServiceWorkerGlobalScope } from './types';

// Entry point executed in the Service Worker global scope
registerServiceWorker(self as unknown as ServiceWorkerGlobalScope);

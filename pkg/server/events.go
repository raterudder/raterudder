package server

// Structured log event names emitted under the "event" JSON field for
// monitoring, heartbeat alerting, and product activity dashboards.
const (
	eventSiteUpdateHeartbeat            = "site_update_heartbeat"
	eventUserCreated                    = "user_created"
	eventUserDeleted                    = "user_deleted"
	eventSiteCreated                    = "site_created"
	eventSiteDeleted                    = "site_deleted"
	eventUserJoinedSite                 = "user_joined_site"
	eventESSConnected                   = "ess_connected"
	eventUtilityConfigured              = "utility_configured"
	eventSiteAutomationPaused           = "site_automation_paused"
	eventSiteAutomationResumed          = "site_automation_resumed"
	eventNotificationDeviceSubscribed   = "notification_device_subscribed"
	eventNotificationDeviceUnsubscribed = "notification_device_unsubscribed"
	eventFeedbackSubmitted              = "feedback_submitted"
	eventInterestSubmitted              = "interest_submitted"
	eventAppConnected                   = "app_connected"
	eventAppRevoked                     = "app_revoked"
)

package httputil

import "time"

// SSEHeartbeatInterval is how often a long-lived Server-Sent Events response
// should emit a comment frame during silence. It is comfortably under the
// reference ingress-nginx default proxy read timeout (60s), so an idle stream
// stays open instead of being closed and read by the client as a dead one.
const SSEHeartbeatInterval = 25 * time.Second

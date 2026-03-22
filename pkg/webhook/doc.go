// Package webhook provides an HTTP client for delivering outgoing webhook
// events with HMAC-SHA256 signatures and configurable retry logic.
//
// The signature scheme follows industry conventions (Stripe, Svix): the
// recipient can verify authenticity by recomputing HMAC-SHA256 over the
// concatenation of the delivery ID, timestamp, and raw body.
//
// Basic usage:
//
//	client := webhook.NewClient()
//	result, err := client.Send(ctx, &webhook.Delivery{
//		URL:     "https://example.com/webhook",
//		Secret:  "whsec_abc123",
//		Event:   "user.created",
//		Payload: jsonBytes,
//	})
//
// With retry:
//
//	result, err := client.SendWithRetry(ctx, &webhook.Delivery{
//		URL:     "https://example.com/webhook",
//		Secret:  "whsec_abc123",
//		Event:   "user.created",
//		Payload: jsonBytes,
//	})
package webhook

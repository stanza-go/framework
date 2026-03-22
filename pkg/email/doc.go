// Package email provides a simple email-sending client backed by the Resend
// HTTP API. It is built entirely on Go's standard library with zero external
// dependencies.
//
// Basic usage:
//
//	client := email.New("re_your_api_key",
//		email.WithFrom("noreply@example.com"),
//	)
//	err := client.Send(ctx, email.Message{
//		To:      []string{"user@example.com"},
//		Subject: "Welcome",
//		HTML:    "<h1>Hello!</h1>",
//	})
package email

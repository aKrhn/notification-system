package domain

import (
	"strings"
	"testing"
)

func BenchmarkValidate_SMS(b *testing.B) {
	req := &CreateNotificationRequest{
		Recipient: "+905551234567",
		Channel:   ChannelSMS,
		Content:   "Hello world",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req.Priority = "" // reset so default is set each time
		req.Validate()
	}
}

func BenchmarkValidate_Email(b *testing.B) {
	subject := "Test Subject"
	req := &CreateNotificationRequest{
		Recipient: "user@example.com",
		Channel:   ChannelEmail,
		Content:   "Hello from email",
		Subject:   &subject,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req.Priority = ""
		req.Validate()
	}
}

func BenchmarkValidate_SMS_MaxLength(b *testing.B) {
	req := &CreateNotificationRequest{
		Recipient: "+905551234567",
		Channel:   ChannelSMS,
		Content:   strings.Repeat("x", 160),
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req.Priority = ""
		req.Validate()
	}
}

func BenchmarkValidate_Batch_100(b *testing.B) {
	notifications := make([]CreateNotificationRequest, 100)
	for i := range notifications {
		notifications[i] = CreateNotificationRequest{
			Recipient: "+905551234567",
			Channel:   ChannelSMS,
			Content:   "Batch item",
		}
	}
	req := &BatchCreateRequest{Notifications: notifications}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := range req.Notifications {
			req.Notifications[j].Priority = ""
		}
		req.Validate()
	}
}

func BenchmarkValidate_WithErrors(b *testing.B) {
	req := &CreateNotificationRequest{
		Channel: "invalid",
		Content: "",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req.Validate()
	}
}

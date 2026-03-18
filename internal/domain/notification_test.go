package domain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestCreateNotificationRequest_Validate(t *testing.T) {
	subject := "Test Subject"

	tests := []struct {
		name      string
		req       CreateNotificationRequest
		wantErr   bool
		wantField string // expected field in error, empty if no specific check
	}{
		{
			name:    "valid SMS",
			req:     CreateNotificationRequest{Recipient: "+905551234567", Channel: ChannelSMS, Content: "Hello"},
			wantErr: false,
		},
		{
			name:    "valid email",
			req:     CreateNotificationRequest{Recipient: "a@b.com", Channel: ChannelEmail, Content: "Hello", Subject: &subject},
			wantErr: false,
		},
		{
			name:    "valid push",
			req:     CreateNotificationRequest{Recipient: "device-token", Channel: ChannelPush, Content: "Hello"},
			wantErr: false,
		},
		{
			name:      "missing recipient",
			req:       CreateNotificationRequest{Channel: ChannelSMS, Content: "Hello"},
			wantErr:   true,
			wantField: "recipient",
		},
		{
			name:      "missing channel",
			req:       CreateNotificationRequest{Recipient: "+90555", Content: "Hello"},
			wantErr:   true,
			wantField: "channel",
		},
		{
			name:      "invalid channel",
			req:       CreateNotificationRequest{Recipient: "+90555", Channel: "fax", Content: "Hello"},
			wantErr:   true,
			wantField: "channel",
		},
		{
			name:      "missing content",
			req:       CreateNotificationRequest{Recipient: "+90555", Channel: ChannelSMS},
			wantErr:   true,
			wantField: "content",
		},
		{
			name:      "SMS too long",
			req:       CreateNotificationRequest{Recipient: "+90555", Channel: ChannelSMS, Content: strings.Repeat("x", 161)},
			wantErr:   true,
			wantField: "content",
		},
		{
			name:    "SMS at limit",
			req:     CreateNotificationRequest{Recipient: "+90555", Channel: ChannelSMS, Content: strings.Repeat("x", 160)},
			wantErr: false,
		},
		{
			name:      "email too long",
			req:       CreateNotificationRequest{Recipient: "a@b.com", Channel: ChannelEmail, Content: strings.Repeat("x", 100001), Subject: &subject},
			wantErr:   true,
			wantField: "content",
		},
		{
			name:      "push too long",
			req:       CreateNotificationRequest{Recipient: "token", Channel: ChannelPush, Content: strings.Repeat("x", 4097)},
			wantErr:   true,
			wantField: "content",
		},
		{
			name:      "email missing subject",
			req:       CreateNotificationRequest{Recipient: "a@b.com", Channel: ChannelEmail, Content: "Hello"},
			wantErr:   true,
			wantField: "subject",
		},
		{
			name:      "invalid priority",
			req:       CreateNotificationRequest{Recipient: "+90555", Channel: ChannelSMS, Content: "Hi", Priority: "urgent"},
			wantErr:   true,
			wantField: "priority",
		},
		{
			name: "default priority",
			req:  CreateNotificationRequest{Recipient: "+90555", Channel: ChannelSMS, Content: "Hi"},
			// After validation, priority should be "normal"
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				var ve *ErrValidation
				if !errors.As(err, &ve) {
					t.Fatalf("expected ErrValidation, got %T", err)
				}
				if tt.wantField != "" {
					found := false
					for _, fe := range ve.Fields {
						if fe.Field == tt.wantField {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected error on field %q, got fields: %v", tt.wantField, ve.Fields)
					}
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}

	// Verify default priority is set
	t.Run("priority defaults to normal", func(t *testing.T) {
		req := CreateNotificationRequest{Recipient: "+90555", Channel: ChannelSMS, Content: "Hi"}
		_ = req.Validate()
		if req.Priority != PriorityNormal {
			t.Errorf("expected priority %q, got %q", PriorityNormal, req.Priority)
		}
	})
}

func TestBatchCreateRequest_Validate(t *testing.T) {
	t.Run("empty batch", func(t *testing.T) {
		req := BatchCreateRequest{}
		err := req.Validate()
		if err == nil {
			t.Fatal("expected error for empty batch")
		}
	})

	t.Run("batch too large", func(t *testing.T) {
		notifications := make([]CreateNotificationRequest, MaxBatchSize+1)
		for i := range notifications {
			notifications[i] = CreateNotificationRequest{Recipient: "+90555", Channel: ChannelSMS, Content: "Hi"}
		}
		req := BatchCreateRequest{Notifications: notifications}
		err := req.Validate()
		if err == nil {
			t.Fatal("expected error for oversized batch")
		}
	})

	t.Run("batch with invalid items", func(t *testing.T) {
		req := BatchCreateRequest{
			Notifications: []CreateNotificationRequest{
				{Recipient: "+90555", Channel: ChannelSMS, Content: "Valid"},
				{Channel: ChannelSMS, Content: "Missing recipient"},
			},
		}
		err := req.Validate()
		if err == nil {
			t.Fatal("expected error for invalid items")
		}
		var ve *ErrValidation
		if !errors.As(err, &ve) {
			t.Fatalf("expected ErrValidation, got %T", err)
		}
		found := false
		for _, fe := range ve.Fields {
			if fe.Field == fmt.Sprintf("notifications[%d].recipient", 1) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected indexed field error, got: %v", ve.Fields)
		}
	})

	t.Run("valid batch", func(t *testing.T) {
		req := BatchCreateRequest{
			Notifications: []CreateNotificationRequest{
				{Recipient: "+90555", Channel: ChannelSMS, Content: "Hi"},
				{Recipient: "+90556", Channel: ChannelSMS, Content: "Hey"},
			},
		}
		if err := req.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

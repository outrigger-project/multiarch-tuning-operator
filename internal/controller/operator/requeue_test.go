package operator

import (
	"errors"
	"testing"
	"time"
)

func TestRequeueAfterError_ErrorsAs(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		msg      string
	}{
		{
			name:     "5s requeue for progressing",
			duration: 5 * time.Second,
			msg:      clusterPodPlacementConfigNotReady,
		},
		{
			name:     "10s requeue for ungating",
			duration: 10 * time.Second,
			msg:      waitingForUngatingPodsError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newRequeueAfterError(tt.duration, tt.msg)
			if err == nil {
				t.Fatal("expected non-nil error")
			}
			if err.Error() != tt.msg {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.msg)
			}

			var requeueErr *requeueAfterError
			if !errors.As(err, &requeueErr) {
				t.Fatal("errors.As should unwrap requeueAfterError")
			}
			if requeueErr.duration != tt.duration {
				t.Errorf("duration = %v, want %v", requeueErr.duration, tt.duration)
			}
			if requeueErr.msg != tt.msg {
				t.Errorf("msg = %q, want %q", requeueErr.msg, tt.msg)
			}
		})
	}
}

func TestRequeueAfterError_NotMatchPlainError(t *testing.T) {
	plainErr := errors.New("some error")
	var requeueErr *requeueAfterError
	if errors.As(plainErr, &requeueErr) {
		t.Fatal("errors.As should not match plain error as requeueAfterError")
	}
}

func TestMergeWithStatusErr(t *testing.T) {
	primaryErr := errors.New("primary failure")
	plainStatusErr := errors.New("status update failed")

	tests := []struct {
		name           string
		statusErr      error
		primaryErrs    []error
		wantRequeue    bool
		wantRequeueDur time.Duration
		wantNil        bool
	}{
		{
			name:           "requeueAfterError takes priority over primary errors",
			statusErr:      newRequeueAfterError(5*time.Second, "progressing"),
			primaryErrs:    []error{primaryErr},
			wantRequeue:    true,
			wantRequeueDur: 5 * time.Second,
		},
		{
			name:        "plain status error aggregates with primary",
			statusErr:   plainStatusErr,
			primaryErrs: []error{primaryErr},
			wantRequeue: false,
		},
		{
			name:        "nil status error returns primary only",
			statusErr:   nil,
			primaryErrs: []error{primaryErr},
			wantRequeue: false,
		},
		{
			name:        "nil status and nil primary returns nil",
			statusErr:   nil,
			primaryErrs: nil,
			wantNil:     true,
		},
		{
			name:           "requeueAfterError with multiple primary errors",
			statusErr:      newRequeueAfterError(5*time.Second, "progressing"),
			primaryErrs:    []error{primaryErr, errors.New("another error")},
			wantRequeue:    true,
			wantRequeueDur: 5 * time.Second,
		},
		{
			name:           "requeueAfterError in primary errors takes priority when the status error is nil",
			statusErr:      nil,
			primaryErrs:    []error{primaryErr, newRequeueAfterError(3*time.Second, "cleanup in progress")},
			wantRequeue:    true,
			wantRequeueDur: 3 * time.Second,
		},
		{
			name:           "requeueAfterError in primary errors takes priority over a plain status error",
			statusErr:      plainStatusErr,
			primaryErrs:    []error{nil, newRequeueAfterError(3*time.Second, "cleanup in progress")},
			wantRequeue:    true,
			wantRequeueDur: 3 * time.Second,
		},
		{
			name:           "status requeueAfterError takes priority over a primary requeueAfterError",
			statusErr:      newRequeueAfterError(5*time.Second, "progressing"),
			primaryErrs:    []error{newRequeueAfterError(3*time.Second, "cleanup in progress")},
			wantRequeue:    true,
			wantRequeueDur: 5 * time.Second,
		},
		{
			name:        "nil primary errors are ignored",
			statusErr:   nil,
			primaryErrs: []error{nil, nil},
			wantNil:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeWithStatusErr(tt.statusErr, tt.primaryErrs...)

			if tt.wantNil {
				if result != nil {
					t.Fatalf("expected nil, got %v", result)
				}
				return
			}

			if result == nil {
				t.Fatal("expected non-nil error")
			}

			var requeueErr *requeueAfterError
			gotRequeue := errors.As(result, &requeueErr)
			if gotRequeue != tt.wantRequeue {
				t.Fatalf("errors.As(requeueAfterError) = %v, want %v", gotRequeue, tt.wantRequeue)
			}
			if gotRequeue && requeueErr.duration != tt.wantRequeueDur {
				t.Fatalf("requeue duration = %v, want %v", requeueErr.duration, tt.wantRequeueDur)
			}
		})
	}
}

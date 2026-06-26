package target

import "testing"

func TestVolumeDetachComplete(t *testing.T) {
	tests := []struct {
		name            string
		status          string
		attachmentCount int
		devicePath      string
		want            bool
	}{
		{
			name:   "available without attachments or device",
			status: "available",
			want:   true,
		},
		{
			name:            "available with attachment",
			status:          "available",
			attachmentCount: 1,
			want:            false,
		},
		{
			name:       "available with local device",
			status:     "available",
			devicePath: "/dev/sdb",
			want:       false,
		},
		{
			name:   "still detaching",
			status: "detaching",
			want:   false,
		},
		{
			name:   "case insensitive available",
			status: "AVAILABLE",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := volumeDetachComplete(tt.status, tt.attachmentCount, tt.devicePath)
			if got != tt.want {
				t.Fatalf("volumeDetachComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVolumeDetachedInOpenStack(t *testing.T) {
	tests := []struct {
		name            string
		status          string
		attachmentCount int
		want            bool
	}{
		{
			name:   "available without attachments",
			status: "available",
			want:   true,
		},
		{
			name:            "available with attachment",
			status:          "available",
			attachmentCount: 1,
			want:            false,
		},
		{
			name:   "detaching without attachments",
			status: "detaching",
			want:   false,
		},
		{
			name:   "case insensitive available",
			status: "AVAILABLE",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := volumeDetachedInOpenStack(tt.status, tt.attachmentCount)
			if got != tt.want {
				t.Fatalf("volumeDetachedInOpenStack() = %v, want %v", got, tt.want)
			}
		})
	}
}

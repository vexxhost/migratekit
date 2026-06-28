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

func TestVolumeAttachComplete(t *testing.T) {
	tests := []struct {
		name            string
		status          string
		attachmentCount int
		devicePath      string
		want            bool
	}{
		{
			name:            "in use with attachment and device",
			status:          "in-use",
			attachmentCount: 1,
			devicePath:      "/dev/sdb",
			want:            true,
		},
		{
			name:       "in use with device but no attachment",
			status:     "in-use",
			devicePath: "/dev/sdb",
			want:       false,
		},
		{
			name:            "in use with attachment but no device",
			status:          "in-use",
			attachmentCount: 1,
			want:            false,
		},
		{
			name:            "available with attachment and device",
			status:          "available",
			attachmentCount: 1,
			devicePath:      "/dev/sdb",
			want:            false,
		},
		{
			name:            "case insensitive in use",
			status:          "IN-USE",
			attachmentCount: 1,
			devicePath:      "/dev/sdb",
			want:            true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := volumeAttachComplete(tt.status, tt.attachmentCount, tt.devicePath)
			if got != tt.want {
				t.Fatalf("volumeAttachComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVolumeAttachedInOpenStack(t *testing.T) {
	tests := []struct {
		name            string
		status          string
		attachmentCount int
		want            bool
	}{
		{
			name:            "in use with attachment",
			status:          "in-use",
			attachmentCount: 1,
			want:            true,
		},
		{
			name:   "in use without attachment",
			status: "in-use",
			want:   false,
		},
		{
			name:            "attaching with attachment",
			status:          "attaching",
			attachmentCount: 1,
			want:            false,
		},
		{
			name:            "case insensitive in use",
			status:          "IN-USE",
			attachmentCount: 1,
			want:            true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := volumeAttachedInOpenStack(tt.status, tt.attachmentCount)
			if got != tt.want {
				t.Fatalf("volumeAttachedInOpenStack() = %v, want %v", got, tt.want)
			}
		})
	}
}

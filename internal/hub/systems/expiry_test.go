//go:build testing

package systems

import (
	"testing"
	"time"
)

func TestComputeRenewal(t *testing.T) {
	now := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		end   time.Time
		cycle string
		want  time.Time
	}{
		{
			name:  "month not yet expired renews from end",
			end:   time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
			cycle: "month",
			want:  time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "year not yet expired renews from end",
			end:   time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
			cycle: "year",
			want:  time.Date(2027, 8, 5, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "month expired renews from today",
			end:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			cycle: "month",
			want:  time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "year expired renews from today",
			end:   time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC),
			cycle: "year",
			want:  time.Date(2027, 7, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "empty cycle defaults to month",
			end:   time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
			cycle: "",
			want:  time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeRenewal(tt.end, tt.cycle, now)
			if !got.Equal(tt.want) {
				t.Errorf("computeRenewal(%v, %q, %v) = %v, want %v", tt.end, tt.cycle, now, got, tt.want)
			}
		})
	}
}

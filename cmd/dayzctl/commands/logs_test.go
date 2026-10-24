package commands

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestParseLogArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want LogOptions
	}{
		{
			name: "explicit all",
			args: []string{"all"},
			want: LogOptions{Lines: DefaultLogLines, Instances: []string{"all"}},
		},
		{
			name: "options only default to all instances",
			args: []string{"-f"},
			want: LogOptions{Lines: DefaultLogLines, Follow: true},
		},
		{
			name: "instance only",
			args: []string{"nasdara"},
			want: LogOptions{Lines: DefaultLogLines, Instances: []string{"nasdara"}},
		},
		{
			name: "follow before instance",
			args: []string{"-f", "nasdara"},
			want: LogOptions{Lines: DefaultLogLines, Follow: true, Instances: []string{"nasdara"}},
		},
		{
			name: "follow after instance",
			args: []string{"nasdara", "--follow"},
			want: LogOptions{Lines: DefaultLogLines, Follow: true, Instances: []string{"nasdara"}},
		},
		{
			name: "tail style line count and follow",
			args: []string{"nasdara", "-200f"},
			want: LogOptions{Lines: "200", Follow: true, Instances: []string{"nasdara"}},
		},
		{
			name: "bare line count",
			args: []string{"-50"},
			want: LogOptions{Lines: "50"},
		},
		{
			name: "n with separate value",
			args: []string{"-n", "50", "nasdara"},
			want: LogOptions{Lines: "50", Instances: []string{"nasdara"}},
		},
		{
			name: "n with attached value",
			args: []string{"-n50", "nasdara"},
			want: LogOptions{Lines: "50", Instances: []string{"nasdara"}},
		},
		{
			name: "cluster f then n with next argument",
			args: []string{"-fn", "25"},
			want: LogOptions{Lines: "25", Follow: true},
		},
		{
			name: "cluster f then n with attached value",
			args: []string{"-fn25"},
			want: LogOptions{Lines: "25", Follow: true},
		},
		{
			name: "all lines",
			args: []string{"-n", "all", "nasdara"},
			want: LogOptions{Lines: "all", Instances: []string{"nasdara"}},
		},
		{
			name: "long options",
			args: []string{"--lines=25", "--since", "2 hours ago", "--until=now", "--follow", "all"},
			want: LogOptions{Lines: "25", Since: "2 hours ago", Until: "now", Follow: true, Instances: []string{"all"}},
		},
		{
			name: "end of options",
			args: []string{"--", "-weird"},
			want: LogOptions{Lines: DefaultLogLines, Instances: []string{"-weird"}},
		},
		{
			name: "multiple instances",
			args: []string{"a", "b"},
			want: LogOptions{Lines: DefaultLogLines, Instances: []string{"a", "b"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLogArgs(tt.args)
			if err != nil {
				t.Fatalf("ParseLogArgs(%v) returned error: %v", tt.args, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseLogArgs(%v) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}

func TestParseLogArgsErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantSub string
	}{
		{"unknown short option", []string{"-z"}, "unknown option: -z"},
		{"unknown long option", []string{"--bogus"}, "unknown option: --bogus"},
		{"missing n value", []string{"-n"}, "requires a value"},
		{"missing lines value", []string{"--lines"}, "requires a value"},
		{"invalid line count", []string{"-n", "abc"}, "invalid line count"},
		{"negative line count", []string{"--lines=-5"}, "invalid line count"},
		{"follow with value", []string{"--follow=yes"}, "does not take a value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseLogArgs(tt.args)
			if err == nil {
				t.Fatalf("ParseLogArgs(%v) expected error, got nil", tt.args)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("ParseLogArgs(%v) error = %q, want it to contain %q", tt.args, err, tt.wantSub)
			}
		})
	}
}

func TestParseLogArgsHelp(t *testing.T) {
	cases := [][]string{
		nil,
		{},
		{"-h"},
		{"--help"},
		{"help"},
	}
	for _, args := range cases {
		if _, err := ParseLogArgs(args); err != ErrHelpRequested {
			t.Fatalf("ParseLogArgs(%v) = %v, want ErrHelpRequested", args, err)
		}
	}
}

func TestBuildJournalctlArgs(t *testing.T) {
	tests := []struct {
		name  string
		opts  LogOptions
		units []string
		want  []string
	}{
		{
			name:  "lines and follow",
			opts:  LogOptions{Lines: "100", Follow: true},
			units: []string{"dayz@nasdara.service"},
			want:  []string{"--unit", "dayz@nasdara.service", "--lines", "100", "--follow"},
		},
		{
			name:  "all lines omits the limit",
			opts:  LogOptions{Lines: "all"},
			units: []string{"dayz@nasdara.service"},
			want:  []string{"--unit", "dayz@nasdara.service"},
		},
		{
			name:  "multiple units with time range",
			opts:  LogOptions{Lines: "20", Since: "today", Until: "now"},
			units: []string{"dayz@a.service", "dayz@b.service"},
			want: []string{
				"--unit", "dayz@a.service",
				"--unit", "dayz@b.service",
				"--lines", "20",
				"--since", "today",
				"--until", "now",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildJournalctlArgs(tt.opts, tt.units)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("buildJournalctlArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunJournalctlUsesJournalctlCommand(t *testing.T) {
	var gotArgs []string
	orig := journalctlCommand
	journalctlCommand = func(args ...string) *exec.Cmd {
		gotArgs = args
		// "true" exits 0 without producing output.
		return exec.Command("true")
	}
	t.Cleanup(func() { journalctlCommand = orig })

	if err := runJournalctl([]string{"--unit", "dayz@test.service", "--lines", "5"}); err != nil {
		t.Fatalf("runJournalctl() = %v, want nil", err)
	}
	want := []string{"--unit", "dayz@test.service", "--lines", "5"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("journalctl received %v, want %v", gotArgs, want)
	}
}

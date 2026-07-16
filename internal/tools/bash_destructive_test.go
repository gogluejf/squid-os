package tools

import "testing"

func TestIsBashCommandDestructive(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		// Read-only basics.
		{"empty", "", false},
		{"cat file", "cat README.md", false},
		{"grep rm text", "grep rm README.md", false},
		{"echo rm text", "echo rm -rf /", false},
		{"find read only", "find . -name '*.go'", false},
		{"git status", "git status --short", false},
		{"git diff", "git diff -- internal/tools/bash.go", false},
		{"git log", "git log --oneline -5", false},
		{"curl get", "curl https://example.com", false},
		{"tar list", "tar -tf archive.tar", false},
		{"systemctl status", "systemctl status squid-os", false},
		{"docker ps", "docker ps", false},
		{"kubectl get", "kubectl get pods", false},
		{"terraform output", "terraform output", false},
		{"quoted operator", "grep 'a > b' README.md", false},

		// File deletion and mutation.
		{"rm file", "rm file.txt", true},
		{"rm rf", "rm -rf ./dist", true},
		{"path rm", "/bin/rm file.txt", true},
		{"sudo rm", "sudo -S rm file.txt", true},
		{"touch", "touch file.txt", true},
		{"mkdir", "mkdir -p foo", true},
		{"chmod", "chmod +x script.sh", true},
		{"mv", "mv a b", true},
		{"cp", "cp a b", true},
		{"tee", "echo hi | tee file.txt", true},
		{"truncate", "truncate -s 0 file.txt", true},
		{"redirect overwrite", "echo hi > file.txt", true},
		{"redirect append", "echo hi >> file.txt", true},
		{"stderr redirect", "cmd 2> errors.txt", true},
		{"clobber redirect", "echo hi >| file.txt", true},

		// In-place editing.
		{"sed print", "sed 's/a/b/' file.txt", false},
		{"sed inplace", "sed -i 's/a/b/' file.txt", true},
		{"sed inplace suffix", "sed -i.bak 's/a/b/' file.txt", true},
		{"gnu sed inplace", "gsed --in-place 's/a/b/' file.txt", true},
		{"perl print", "perl -pe 's/a/b/' file.txt", false},
		{"perl inplace", "perl -pi -e 's/a/b/' file.txt", true},

		// Find and xargs.
		{"find delete", "find . -name '*.tmp' -delete", true},
		{"find exec rm", "find . -name '*.tmp' -exec rm {} ;", true},
		{"xargs grep", "printf '%s\\n' a | xargs grep foo", false},
		{"xargs rm", "printf '%s\\n' a | xargs rm", true},

		// Git.
		{"git add", "git add .", true},
		{"git commit", "git commit -m 'x'", true},
		{"git clean", "git clean -fd", true},
		{"git reset mixed", "git reset HEAD~1", false},
		{"git reset hard", "git reset --hard HEAD", true},
		{"git branch list", "git branch", false},
		{"git branch delete", "git branch -D old", true},
		{"git push", "git push origin main", true},

		// Network and remote mutation.
		{"curl post compact", "curl -XPOST https://example.com", true},
		{"curl post separated", "curl -X POST https://example.com", true},
		{"curl data", "curl -d 'a=b' https://example.com", true},
		{"curl output", "curl -o file https://example.com", true},
		{"wget", "wget https://example.com/file", true},

		// Packages/builds.
		{"npm view", "npm view react version", false},
		{"npm install", "npm install", true},
		{"npm run", "npm run build", true},
		{"go test", "go test ./...", false},
		{"go build", "go build ./...", true},
		{"pip install", "pip install requests", true},
		{"brew list", "brew list", false},
		{"brew install", "brew install jq", true},
		{"make", "make", true},

		// System/container/archive subcommands.
		{"systemctl restart", "systemctl restart app", true},
		{"docker run", "docker run alpine", true},
		{"kubectl apply", "kubectl apply -f deploy.yaml", true},
		{"helm uninstall", "helm uninstall app", true},
		{"terraform plan", "terraform plan", true},
		{"tar extract", "tar -xf archive.tar", true},
		{"xz decompress", "xz -d archive.xz", true},

		// Database mutation.
		{"psql select", "psql -c 'select * from users'", false},
		{"psql drop", "psql -c 'drop table users'", true},
		{"redis get", "redis-cli get key", false},
		{"redis set", "redis-cli set key value", true},

		// Compound commands.
		{"compound read only", "git status && grep foo README.md", false},
		{"compound destructive second", "git status && rm file.txt", true},
		{"env assignment", "FOO=bar rm file.txt", true},
		{"command prefix", "command rm file.txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsBashCommandDestructive(tt.cmd)
			if got != tt.want {
				t.Fatalf("IsBashCommandDestructive(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

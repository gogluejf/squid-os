package tools

import "strings"

var bashAlwaysDestructiveCommands = map[string]struct{}{
	"rm":         {},
	"rmdir":      {},
	"unlink":     {},
	"shred":      {},
	"wipe":       {},
	"srm":        {},
	"touch":      {},
	"mkdir":      {},
	"mkfifo":     {},
	"mknod":      {},
	"ln":         {},
	"mv":         {},
	"cp":         {},
	"truncate":   {},
	"tee":        {},
	"chmod":      {},
	"chown":      {},
	"chgrp":      {},
	"setfacl":    {},
	"chattr":     {},
	"xattr":      {},
	"setfattr":   {},
	"install":    {},
	"patch":      {},
	"wget":       {},
	"scp":        {},
	"sftp":       {},
	"ftp":        {},
	"nc":         {},
	"netcat":     {},
	"socat":      {},
	"kill":       {},
	"killall":    {},
	"pkill":      {},
	"shutdown":   {},
	"reboot":     {},
	"halt":       {},
	"poweroff":   {},
	"mount":      {},
	"umount":     {},
	"swapon":     {},
	"swapoff":    {},
	"modprobe":   {},
	"rmmod":      {},
	"insmod":     {},
	"crontab":    {},
	"at":         {},
	"batch":      {},
	"mkfs":       {},
	"fsck":       {},
	"fdisk":      {},
	"parted":     {},
	"sfdisk":     {},
	"gdisk":      {},
	"sgdisk":     {},
	"wipefs":     {},
	"blkdiscard": {},
	"cryptsetup": {},
	"losetup":    {},
	"createdb":   {},
	"dropdb":     {},
	"createuser": {},
	"dropuser":   {},
	"unzip":      {},
	"gunzip":     {},
	"bunzip2":    {},
	"7z":         {},
	"rar":        {},
	"cpio":       {},
	"make":       {},
	"ninja":      {},
	"gradle":     {},
	"mvn":        {},
}

var bashDestructiveSubcommands = map[string]map[string]struct{}{
	"git": {
		"add": {}, "rm": {}, "mv": {}, "commit": {}, "clean": {}, "restore": {},
		"switch": {}, "merge": {}, "rebase": {}, "cherry-pick": {}, "revert": {},
		"stash": {}, "tag": {}, "push": {}, "pull": {}, "fetch": {}, "submodule": {},
		"gc": {},
	},
	"apt":       {"install": {}, "remove": {}, "purge": {}, "upgrade": {}, "update": {}, "autoremove": {}},
	"apt-get":   {"install": {}, "remove": {}, "purge": {}, "upgrade": {}, "update": {}, "autoremove": {}},
	"dnf":       {"install": {}, "remove": {}, "upgrade": {}, "update": {}},
	"yum":       {"install": {}, "remove": {}, "upgrade": {}, "update": {}},
	"pacman":    {"-s": {}, "-r": {}, "-u": {}, "-syu": {}},
	"apk":       {"add": {}, "del": {}, "upgrade": {}, "update": {}},
	"brew":      {"install": {}, "uninstall": {}, "remove": {}, "upgrade": {}, "update": {}, "tap": {}, "untap": {}},
	"pip":       {"install": {}, "uninstall": {}},
	"pip3":      {"install": {}, "uninstall": {}},
	"pipx":      {"install": {}, "uninstall": {}, "upgrade": {}, "inject": {}},
	"npm":       {"install": {}, "i": {}, "uninstall": {}, "remove": {}, "update": {}, "ci": {}, "audit": {}, "run": {}},
	"yarn":      {"add": {}, "remove": {}, "install": {}, "upgrade": {}, "run": {}},
	"pnpm":      {"add": {}, "remove": {}, "install": {}, "update": {}, "run": {}},
	"bun":       {"add": {}, "remove": {}, "install": {}, "run": {}},
	"cargo":     {"install": {}, "add": {}, "remove": {}, "build": {}},
	"go":        {"get": {}, "install": {}, "build": {}},
	"composer":  {"install": {}, "update": {}, "require": {}, "remove": {}},
	"gem":       {"install": {}, "uninstall": {}, "update": {}},
	"bundle":    {"install": {}, "update": {}},
	"poetry":    {"add": {}, "remove": {}, "install": {}, "update": {}},
	"uv":        {"add": {}, "remove": {}, "pip": {}},
	"conda":     {"install": {}, "remove": {}, "update": {}, "upgrade": {}},
	"gh":        {"repo": {}, "issue": {}, "pr": {}, "release": {}, "workflow": {}, "api": {}},
	"glab":      {"repo": {}, "issue": {}, "mr": {}, "release": {}, "pipeline": {}, "api": {}},
	"systemctl": {"start": {}, "stop": {}, "restart": {}, "reload": {}, "enable": {}, "disable": {}, "mask": {}, "unmask": {}},
	"service":   {"start": {}, "stop": {}, "restart": {}, "reload": {}},
	"docker":    {"run": {}, "build": {}, "pull": {}, "push": {}, "rm": {}, "rmi": {}, "compose": {}, "volume": {}, "system": {}},
	"podman":    {"run": {}, "build": {}, "pull": {}, "push": {}, "rm": {}, "rmi": {}, "compose": {}, "volume": {}, "system": {}},
	"kubectl":   {"apply": {}, "delete": {}, "edit": {}, "patch": {}, "scale": {}, "rollout": {}},
	"helm":      {"install": {}, "upgrade": {}, "uninstall": {}, "delete": {}},
	"terraform": {"apply": {}, "destroy": {}, "init": {}, "plan": {}},
	"pulumi":    {"up": {}, "destroy": {}, "preview": {}},
	"tar":       {"-x": {}, "--extract": {}},
	"xz":        {"-d": {}, "--decompress": {}},
}

// IsBashCommandDestructive classifies bash commands that obviously mutate local state,
// remote state, or the filesystem. It is intentionally conservative and complements
// the model-provided destructive flag; it is not a full shell parser.
func IsBashCommandDestructive(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}

	if containsUnquotedRedirection(command) {
		return true
	}

	commands := splitBashCommandList(command)
	for _, cmd := range commands {
		words := shellWords(cmd)
		if len(words) == 0 {
			continue
		}
		if isBashSimpleCommandDestructive(words) {
			return true
		}
	}

	return false
}

func isBashSimpleCommandDestructive(words []string) bool {
	words = stripEnvAssignments(words)
	words = stripCommandPrefix(words)
	if len(words) == 0 {
		return false
	}

	cmd := commandBase(words[0])
	if cmd == "sudo" || cmd == "doas" {
		words = stripSudoFlags(words[1:])
		if len(words) == 0 {
			return false
		}
		cmd = commandBase(words[0])
	}

	if _, ok := bashAlwaysDestructiveCommands[cmd]; ok {
		return true
	}

	switch cmd {
	case "sed", "gsed":
		return hasShortFlagPrefix(words[1:], "-i") || hasAnyFlag(words[1:], "--in-place")
	case "perl", "ruby":
		return hasShortFlagPrefix(words[1:], "-i")
	case "find":
		return hasAnyArg(words[1:], "-delete") || hasExecMutator(words[1:])
	case "xargs":
		return containsKnownMutator(words[1:])
	case "curl", "http":
		return isCurlLikeDestructive(words)
	case "dd":
		return hasPrefixArg(words[1:], "of=")
	case "sysctl":
		return hasAnyFlag(words[1:], "-w")
	case "psql", "mysql", "sqlite3", "mongosh", "redis-cli":
		return containsDatabaseMutation(words[1:])
	case "git":
		return isGitDestructive(words[1:])
	}

	if subs, ok := bashDestructiveSubcommands[cmd]; ok {
		for _, arg := range words[1:] {
			lower := strings.ToLower(arg)
			if _, ok := subs[lower]; ok {
				return true
			}
			if strings.HasPrefix(lower, "-") {
				for sub := range subs {
					if strings.HasPrefix(sub, "-") && !strings.HasPrefix(sub, "--") && strings.Contains(strings.TrimPrefix(lower, "-"), strings.TrimPrefix(sub, "-")) {
						return true
					}
				}
				continue
			}
			return false
		}
	}

	return false
}

func containsUnquotedRedirection(command string) bool {
	var quote rune
	escaped := false
	for i, r := range command {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == '>' {
			return true
		}
		if r == '&' && i+1 < len(command) && command[i+1] == '>' {
			return true
		}
	}
	return false
}

func splitBashCommandList(command string) []string {
	var parts []string
	var b strings.Builder
	var quote rune
	escaped := false

	flush := func() {
		part := strings.TrimSpace(b.String())
		if part != "" {
			parts = append(parts, part)
		}
		b.Reset()
	}

	for _, r := range command {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			b.WriteRune(r)
			escaped = true
			continue
		}
		if quote != 0 {
			b.WriteRune(r)
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			b.WriteRune(r)
			continue
		}
		if r == ';' || r == '|' || r == '&' {
			flush()
			continue
		}
		b.WriteRune(r)
	}
	flush()
	return parts
}

func shellWords(s string) []string {
	var words []string
	var b strings.Builder
	var quote rune
	escaped := false

	flush := func() {
		if b.Len() > 0 {
			words = append(words, b.String())
			b.Reset()
		}
	}

	for _, r := range s {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if strings.ContainsRune(" \t\r\n", r) {
			flush()
			continue
		}
		b.WriteRune(r)
	}
	flush()
	return words
}

func stripEnvAssignments(words []string) []string {
	for len(words) > 0 && strings.Contains(words[0], "=") && !strings.HasPrefix(words[0], "-") {
		name, _, ok := strings.Cut(words[0], "=")
		if !ok || name == "" || strings.ContainsAny(name, "/.") {
			break
		}
		words = words[1:]
	}
	return words
}

func stripCommandPrefix(words []string) []string {
	for len(words) > 0 {
		switch commandBase(words[0]) {
		case "command", "exec", "env", "time":
			words = words[1:]
		default:
			return words
		}
	}
	return words
}

func stripSudoFlags(words []string) []string {
	for len(words) > 0 && strings.HasPrefix(words[0], "-") {
		flag := words[0]
		words = words[1:]
		if flag == "-u" || flag == "-g" || flag == "-p" || flag == "-h" {
			if len(words) > 0 {
				words = words[1:]
			}
		}
	}
	return words
}

func commandBase(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if idx := strings.LastIndex(cmd, "/"); idx >= 0 {
		cmd = cmd[idx+1:]
	}
	return strings.ToLower(cmd)
}

func hasAnyArg(args []string, targets ...string) bool {
	set := map[string]struct{}{}
	for _, target := range targets {
		set[target] = struct{}{}
	}
	for _, arg := range args {
		if _, ok := set[arg]; ok {
			return true
		}
	}
	return false
}

func hasAnyFlag(args []string, flags ...string) bool {
	for _, arg := range args {
		for _, flag := range flags {
			if arg == flag {
				return true
			}
		}
	}
	return false
}

func hasShortFlagPrefix(args []string, prefix string) bool {
	needle := strings.TrimPrefix(prefix, "-")
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return true
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(strings.TrimPrefix(arg, "-"), needle) {
			return true
		}
	}
	return false
}

func hasPrefixArg(args []string, prefix string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return false
}

func hasExecMutator(args []string) bool {
	for i, arg := range args {
		if (arg == "-exec" || arg == "-execdir") && containsKnownMutator(args[i+1:]) {
			return true
		}
	}
	return false
}

func containsKnownMutator(args []string) bool {
	for _, arg := range args {
		cmd := commandBase(arg)
		if _, ok := bashAlwaysDestructiveCommands[cmd]; ok {
			return true
		}
		if _, ok := bashDestructiveSubcommands[cmd]; ok {
			return true
		}
	}
	return false
}

func isCurlLikeDestructive(words []string) bool {
	args := words[1:]
	for i, arg := range args {
		lower := strings.ToLower(arg)
		if lower == "-d" || lower == "--data" || lower == "--data-raw" || lower == "--data-binary" || lower == "--form" || lower == "--form-string" || lower == "-F" || lower == "-o" || lower == "--output" || lower == "-O" || lower == "--remote-name" {
			return true
		}
		if lower == "-x" || lower == "--request" {
			if i+1 < len(args) {
				method := strings.ToUpper(args[i+1])
				return method == "POST" || method == "PUT" || method == "PATCH" || method == "DELETE"
			}
		}
		if strings.HasPrefix(arg, "-X") && len(arg) > 2 {
			method := strings.ToUpper(arg[2:])
			return method == "POST" || method == "PUT" || method == "PATCH" || method == "DELETE"
		}
	}
	return false
}

func isGitDestructive(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		sub := strings.ToLower(arg)
		if sub == "reset" {
			return hasAnyArg(args, "--hard", "--merge", "--keep")
		}
		if sub == "branch" {
			return hasAnyArg(args, "-d", "-D", "--delete")
		}
		_, ok := bashDestructiveSubcommands["git"][sub]
		return ok
	}
	return false
}

func containsDatabaseMutation(args []string) bool {
	joined := " " + strings.ToLower(strings.Join(args, " ")) + " "
	mutations := []string{" insert ", " update ", " delete ", " drop ", " create ", " alter ", " truncate ", " replace ", " flushdb ", " flushall ", " set ", " del "}
	for _, mutation := range mutations {
		if strings.Contains(joined, mutation) {
			return true
		}
	}
	return false
}

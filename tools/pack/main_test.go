//go:build pack

// tools/pack 的单元测试。跑法：
//
//	go test -tags pack ./tools/pack/ -count=1
//
// 平时的 `go test ./...` 不带 tag，会跳过这个包（和 `go vet ./...` 一样）；
// Makefile 的 test 目标里单独跑了这一行，`make check` 会覆盖到。
// 完整的 6 平台打包仍由 `make pack-check` 干跑覆盖；这里只测能定位
// 回归的纯逻辑，外加一个单平台的 buildOne 端到端。
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// repoRoot 找到仓库根（go.mod 所在目录）并 chdir 过去，测试结束恢复。
// readVersion / collectPayload / buildOne 都用相对路径，必须在根目录跑。
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("找不到仓库根（go.mod）")
		}
		dir = parent
	}
	chdir(t, dir)
	return dir
}

// chdir 换目录，测试结束恢复原目录。测试不加 t.Parallel，串行换目录是安全的。
func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestTargetsExactlySixPlatforms(t *testing.T) {
	want := []target{
		{"windows", "amd64"},
		{"windows", "arm64"},
		{"linux", "amd64"},
		{"linux", "arm64"},
		{"darwin", "amd64"},
		{"darwin", "arm64"},
	}
	if len(targets) != len(want) {
		t.Fatalf("平台数 = %d，想要 %d：%v", len(targets), len(want), targets)
	}
	seen := map[target]bool{}
	for i, tgt := range targets {
		if tgt != want[i] {
			t.Errorf("targets[%d] = %v，想要 %v", i, tgt, want[i])
		}
		if seen[tgt] {
			t.Errorf("平台重复：%v", tgt)
		}
		seen[tgt] = true
		switch tgt.os {
		case "windows", "linux", "darwin":
		default:
			t.Errorf("未知系统：%q", tgt.os)
		}
		switch tgt.arch {
		case "amd64", "arm64":
		default:
			t.Errorf("未知架构：%q", tgt.arch)
		}
	}
}

func TestArchiveName(t *testing.T) {
	cases := []struct {
		t    target
		want string
	}{
		{target{"windows", "amd64"}, "minilang_0.1.0_windows_amd64.zip"},
		{target{"windows", "arm64"}, "minilang_0.1.0_windows_arm64.zip"},
		{target{"linux", "amd64"}, "minilang_0.1.0_linux_amd64.tar.gz"},
		{target{"linux", "arm64"}, "minilang_0.1.0_linux_arm64.tar.gz"},
		{target{"darwin", "amd64"}, "minilang_0.1.0_darwin_amd64.tar.gz"},
		{target{"darwin", "arm64"}, "minilang_0.1.0_darwin_arm64.tar.gz"},
	}
	for _, c := range cases {
		if got := archiveName("0.1.0", c.t); got != c.want {
			t.Errorf("archiveName(0.1.0, %v) = %q，想要 %q", c.t, got, c.want)
		}
	}
	// 版本号原样拼进文件名，不做归一化。
	if got := archiveName("0.2.0-rc.1", target{"linux", "amd64"}); got != "minilang_0.2.0-rc.1_linux_amd64.tar.gz" {
		t.Errorf("archiveName(0.2.0-rc.1, linux/amd64) = %q", got)
	}
}

func TestBinaryName(t *testing.T) {
	if got := binaryName("windows"); got != "minilang.exe" {
		t.Errorf("binaryName(windows) = %q，想要 minilang.exe", got)
	}
	for _, os := range []string{"linux", "darwin"} {
		if got := binaryName(os); got != "minilang" {
			t.Errorf("binaryName(%s) = %q，想要 minilang", os, got)
		}
	}
}

func TestChecksumLine(t *testing.T) {
	sum := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	got := checksumLine(sum, filepath.Join("dist", "minilang_0.1.0_linux_amd64.tar.gz"))
	want := sum + "  minilang_0.1.0_linux_amd64.tar.gz"
	if got != want {
		t.Fatalf("checksumLine = %q，想要 %q", got, want)
	}
	// 格式：64 位小写 hex + 恰好两个空格 + 文件名（sha256sum -c 认识的格式）。
	re := regexp.MustCompile(`^[0-9a-f]{64}  [^ /]+$`)
	if !re.MatchString(got) {
		t.Errorf("checksum 行格式不符：%q", got)
	}
	if strings.Contains(got, "\t") {
		t.Errorf("checksum 行里不该有制表符：%q", got)
	}
	// 只写基础名，不带目录。
	if got := checksumLine(sum, filepath.Join("/a", "b", "x.zip")); got != sum+"  x.zip" {
		t.Errorf("checksumLine 应去掉目录：%q", got)
	}
}

func TestSha256File(t *testing.T) {
	dir := t.TempDir()
	abc := filepath.Join(dir, "abc.txt")
	writeFile(t, abc, "abc", 0o644)
	if got, err := sha256File(abc); err != nil || got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("sha256File(abc) = %q, %v", got, err)
	}
	empty := filepath.Join(dir, "empty.txt")
	writeFile(t, empty, "", 0o644)
	if got, err := sha256File(empty); err != nil || got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("sha256File(空文件) = %q, %v", got, err)
	}
	if _, err := sha256File(filepath.Join(dir, "不存在")); err == nil {
		t.Error("sha256File(不存在的文件) 应返回错误")
	}
}

func TestFileSize(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	writeFile(t, p, "12345", 0o644)
	if got, err := fileSize(p); err != nil || got != 5 {
		t.Errorf("fileSize = %d, %v，想要 5", got, err)
	}
	if _, err := fileSize(filepath.Join(dir, "不存在")); err == nil {
		t.Error("fileSize(不存在的文件) 应返回错误")
	}
}

func TestReadVersion(t *testing.T) {
	repoRoot(t)
	got, err := readVersion()
	if err != nil {
		t.Fatalf("readVersion: %v", err)
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(got) {
		t.Errorf("版本号格式不符：%q", got)
	}
	// 和源码里的 const version 是同一份，没有各写一份。
	src, err := os.ReadFile(filepath.Join("cmd", "minilang", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	m := versionRe.FindSubmatch(src)
	if m == nil {
		t.Fatal("cmd/minilang/main.go 里找不到 const version")
	}
	if got != string(m[1]) {
		t.Errorf("readVersion = %q，源码 const version = %q", got, m[1])
	}
}

func TestReadVersionErrors(t *testing.T) {
	// main.go 里没有 const version：报错，而不是静默返回空串。
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "cmd", "minilang", "main.go"), "package main\n", 0o644)
	chdir(t, dir)
	if _, err := readVersion(); err == nil {
		t.Error("找不到 const version 时应返回错误")
	}

	// 连 main.go 都没有：同样报错。
	dir2 := t.TempDir()
	chdir(t, dir2)
	if _, err := readVersion(); err == nil {
		t.Error("读不到 main.go 时应返回错误")
	}
}

func TestCollectPayload(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "bin", "minilang"), "fake-binary", 0o755)
	writeFile(t, filepath.Join(dir, "README.md"), "# minilang", 0o644)
	writeFile(t, filepath.Join(dir, "LICENSE"), "MIT", 0o644)
	writeFile(t, filepath.Join(dir, "docs", "a.md"), "doc", 0o644)
	writeFile(t, filepath.Join(dir, "docs", "b.txt"), "不应打包", 0o644)
	writeFile(t, filepath.Join(dir, "docs", "sub", "c.ml"), "print 1;", 0o644)
	writeFile(t, filepath.Join(dir, "examples", "e.ml"), "print 2;", 0o644)
	writeFile(t, filepath.Join(dir, "examples", "notes.txt"), "不应打包", 0o644)
	chdir(t, dir)

	files, err := collectPayload(filepath.Join(dir, "bin", "minilang"), "minilang")
	if err != nil {
		t.Fatalf("collectPayload: %v", err)
	}
	byName := map[string]payload{}
	for _, f := range files {
		if _, dup := byName[f.name]; dup {
			t.Errorf("压缩包内路径重复：%q", f.name)
		}
		byName[f.name] = f
	}
	// 二进制打头，且可执行。
	if len(files) == 0 || files[0].name != "minilang" || files[0].mode != 0o755 {
		t.Errorf("第一个 payload 应为可执行的二进制，实际 %+v", files[0])
	}
	for _, name := range []string{"README.md", "LICENSE", "docs/a.md", "docs/sub/c.ml", "examples/e.ml"} {
		p, ok := byName[name]
		if !ok {
			t.Errorf("缺 payload：%q", name)
			continue
		}
		if p.mode != 0o644 {
			t.Errorf("%s 权限 = %o，想要 0644", name, p.mode)
		}
		if strings.Contains(name, "\\") {
			t.Errorf("%s 应使用 / 分隔", name)
		}
	}
	// 非 md/ml 不打包。
	for _, name := range []string{"docs/b.txt", "examples/notes.txt"} {
		if _, ok := byName[name]; ok {
			t.Errorf("不应打包：%q", name)
		}
	}
}

func TestCollectPayloadMissingDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "README.md"), "# minilang", 0o644)
	writeFile(t, filepath.Join(dir, "LICENSE"), "MIT", 0o644)
	chdir(t, dir)
	if _, err := collectPayload(filepath.Join(dir, "minilang"), "minilang"); err == nil {
		t.Error("docs/ 不存在时 collectPayload 应返回错误")
	}
}

func TestWriteZipRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src1 := filepath.Join(dir, "a.txt")
	src2 := filepath.Join(dir, "b.txt")
	writeFile(t, src1, "hello", 0o644)
	writeFile(t, src2, "world", 0o755)
	dst := filepath.Join(dir, "out.zip")
	files := []payload{
		{name: "minilang", src: src2, mode: 0o755},
		{name: "README.md", src: src1, mode: 0o644},
	}
	if err := writeZip(dst, "minilang_0.1.0_linux_amd64/", files); err != nil {
		t.Fatalf("writeZip: %v", err)
	}
	zr, err := zip.OpenReader(dst)
	if err != nil {
		t.Fatalf("打不开 zip：%v", err)
	}
	defer zr.Close()
	if len(zr.File) != 2 {
		t.Fatalf("zip 里 %d 个文件，想要 2", len(zr.File))
	}
	want := map[string]struct {
		content string
		mode    os.FileMode
	}{
		"minilang_0.1.0_linux_amd64/minilang":  {"world", 0o755},
		"minilang_0.1.0_linux_amd64/README.md": {"hello", 0o644},
	}
	for _, f := range zr.File {
		w, ok := want[f.Name]
		if !ok {
			t.Errorf("zip 里有多余文件：%q", f.Name)
			continue
		}
		if f.Mode() != w.mode {
			t.Errorf("%s 权限 = %o，想要 %o", f.Name, f.Mode(), w.mode)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != w.content {
			t.Errorf("%s 内容 = %q，想要 %q", f.Name, data, w.content)
		}
	}
}

func TestWriteTarGzRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src1 := filepath.Join(dir, "a.txt")
	src2 := filepath.Join(dir, "b.txt")
	writeFile(t, src1, "hello", 0o644)
	writeFile(t, src2, "world", 0o755)
	dst := filepath.Join(dir, "out.tar.gz")
	files := []payload{
		{name: "minilang", src: src2, mode: 0o755},
		{name: "README.md", src: src1, mode: 0o644},
	}
	if err := writeTarGz(dst, "minilang_0.1.0_linux_amd64/", files); err != nil {
		t.Fatalf("writeTarGz: %v", err)
	}
	f, err := os.Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("打不开 gzip：%v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	want := map[string]struct {
		content string
		mode    int64
	}{
		"minilang_0.1.0_linux_amd64/minilang":  {"world", 0o755},
		"minilang_0.1.0_linux_amd64/README.md": {"hello", 0o644},
	}
	seen := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("读 tar：%v", err)
		}
		seen++
		w, ok := want[hdr.Name]
		if !ok {
			t.Errorf("tar 里有多余文件：%q", hdr.Name)
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			t.Errorf("%s 不是普通文件：typeflag %c", hdr.Name, hdr.Typeflag)
		}
		if hdr.Mode != w.mode {
			t.Errorf("%s 权限 = %o，想要 %o", hdr.Name, hdr.Mode, w.mode)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(data)) != hdr.Size {
			t.Errorf("%s 大小声明 %d，实际 %d", hdr.Name, hdr.Size, len(data))
		}
		if string(data) != w.content {
			t.Errorf("%s 内容 = %q，想要 %q", hdr.Name, data, w.content)
		}
	}
	if seen != 2 {
		t.Errorf("tar 里 %d 个文件，想要 2", seen)
	}
}

func TestVerifyBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell 脚本桩在 Windows 上跑不了")
	}
	dir := t.TempDir()
	ok := filepath.Join(dir, "minilang-ok")
	writeFile(t, ok, "#!/bin/sh\necho 'minilang 0.1.0'\n", 0o755)
	if err := verifyBinary(ok, "0.1.0"); err != nil {
		t.Errorf("verifyBinary(版本相符) = %v，想要 nil", err)
	}

	bad := filepath.Join(dir, "minilang-bad")
	writeFile(t, bad, "#!/bin/sh\necho 'minilang 0.2.0'\n", 0o755)
	if err := verifyBinary(bad, "0.1.0"); err == nil {
		t.Error("verifyBinary(版本不符) 应返回错误")
	}

	if err := verifyBinary(filepath.Join(dir, "不存在"), "0.1.0"); err == nil {
		t.Error("verifyBinary(跑不起来的二进制) 应返回错误")
	}
}

// TestBuildOneSinglePlatform 是唯一的端到端测试：只编本机一个平台，
// 校验产物文件名、落盘位置、压缩包内容与 sha256。
// 完整 6 平台仍由 make pack-check 覆盖（约 1 分钟，不适合放进单元测试）。
func TestBuildOneSinglePlatform(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go 不在 PATH，跳过需要编译的测试")
	}
	root := repoRoot(t)
	version, err := readVersion()
	if err != nil {
		t.Fatalf("readVersion: %v", err)
	}
	_, distStatErr := os.Stat(filepath.Join(root, "dist"))
	distExisted := distStatErr == nil

	tmp := t.TempDir()
	// 和 run() 一致：buildDir 在 distDir 里面，MkdirAll 会顺带建出 distDir。
	distDir := filepath.Join(tmp, "dist")
	buildDir := filepath.Join(distDir, ".build")
	tgt := target{runtime.GOOS, runtime.GOARCH}

	archive, err := buildOne(tgt, version, buildDir, distDir)
	if err != nil {
		t.Fatalf("buildOne(%v): %v", tgt, err)
	}
	// 产物文件名与落盘位置：只在给定的 -out 目录下，不写仓库里的 dist/。
	wantArchive := filepath.Join(distDir, archiveName(version, tgt))
	if archive != wantArchive {
		t.Errorf("buildOne 返回 %q，想要 %q", archive, wantArchive)
	}
	if _, err := os.Stat(archive); err != nil {
		t.Errorf("压缩包不存在：%v", err)
	}
	entries, err := os.ReadDir(distDir)
	if err != nil {
		t.Fatal(err)
	}
	// dist/ 里只有压缩包和中间产物目录 .build（run() 结束时才删 .build，
	// buildOne 本身不管清理——这里只断言东西没写到别处）。
	if len(entries) != 2 {
		t.Errorf("dist/ 里应只有压缩包和 .build，实际 %v", entries)
	}
	if _, err := os.Stat(filepath.Join(buildDir, tgt.os+"_"+tgt.arch, binaryName(tgt.os))); err != nil {
		t.Errorf("中间产物二进制不在 buildDir 里：%v", err)
	}
	if !distExisted {
		if _, err := os.Stat(filepath.Join(root, "dist")); !os.IsNotExist(err) {
			t.Error("buildOne 不应往仓库的 dist/ 写东西")
		}
	}

	// sha256File 算出来的摘要就是文件本身的摘要。
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if got, err := sha256File(archive); err != nil || got != hex.EncodeToString(sum[:]) {
		t.Errorf("sha256File = %q, %v，想要 %x", got, err, sum)
	}

	// 压缩包内容：二进制 + README + LICENSE + 至少一个 .md/.ml。
	names := archiveNames(t, archive)
	if len(names) == 0 {
		t.Fatalf("压缩包是空的")
	}
	prefix := baseName(version, tgt) + "/"
	hasBin, hasReadme, hasLicense, hasDoc := false, false, false, false
	for _, n := range names {
		if !strings.HasPrefix(n, prefix) {
			t.Errorf("压缩包内路径 %q 不带前缀 %q", n, prefix)
		}
		switch strings.TrimPrefix(n, prefix) {
		case binaryName(tgt.os):
			hasBin = true
		case "README.md":
			hasReadme = true
		case "LICENSE":
			hasLicense = true
		}
		if strings.HasSuffix(n, ".md") || strings.HasSuffix(n, ".ml") {
			hasDoc = true
		}
	}
	if !hasBin || !hasReadme || !hasLicense || !hasDoc {
		t.Errorf("压缩包内容不全：二进制 %v、README %v、LICENSE %v、文档 %v；共 %d 个文件",
			hasBin, hasReadme, hasLicense, hasDoc, len(names))
	}
}

// archiveNames 列出压缩包里的文件（zip 与 tar.gz 都支持）。
func archiveNames(t *testing.T, archive string) []string {
	t.Helper()
	if strings.HasSuffix(archive, ".zip") {
		zr, err := zip.OpenReader(archive)
		if err != nil {
			t.Fatalf("打不开 zip：%v", err)
		}
		defer zr.Close()
		var names []string
		for _, f := range zr.File {
			names = append(names, f.Name)
		}
		return names
	}
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("打不开 gzip：%v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("读 tar：%v", err)
		}
		names = append(names, hdr.Name)
	}
	return names
}

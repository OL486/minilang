//go:build ignore

// Command pack 交叉编译 minilang 到多个平台，并打包成可分发的压缩包。
//
// 为什么用 Go 写而不是 shell + zip + tar：
// Windows 默认没有 zip 和 sha256sum，Git Bash 的 tar 对 -z 的支持也看版本，
// 一个 shell 脚本要在三套系统上都跑对，比用标准库写这两百行还费劲。
// 用 Go 写还有个好处：打包逻辑和 `make release`、CI 的 release workflow 是同一份，
// 不会出现「本地出的包和 CI 出的包内容不一致」这种最难查的问题。
//
// 用法（必须在仓库根目录）：
//
//	go run tools/pack/main.go
//
// 产物：
//
//	dist/minilang_<version>_<os>_<arch>.zip      （windows）
//	dist/minilang_<version>_<os>_<arch>.tar.gz   （其他）
//	dist/checksums.txt                           （sha256，可直接 sha256sum -c）
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// target 是一个发布目标。不追求覆盖所有 GOARCH，
// 只发真正有人在用的：服务器和 CI 是 amd64，Apple Silicon 和 ARM 服务器是 arm64。
type target struct {
	os   string
	arch string
}

var targets = []target{
	{"windows", "amd64"},
	{"windows", "arm64"},
	{"linux", "amd64"},
	{"linux", "arm64"},
	{"darwin", "amd64"},
	{"darwin", "arm64"},
}

// payload 是打进压缩包的一个文件。
type payload struct {
	name string // 压缩包内路径，用 / 分隔
	src  string // 磁盘上的源路径
	mode os.FileMode
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "pack: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// 所有相对路径都基于仓库根，跑错目录会得到一堆莫名其妙的错误，
	// 不如一开始就说清楚。
	if _, err := os.Stat("go.mod"); err != nil {
		return fmt.Errorf("请在仓库根目录运行（当前目录找不到 go.mod）")
	}

	version, err := readVersion()
	if err != nil {
		return err
	}
	fmt.Printf("打包 minilang %s，目标 %d 个平台\n\n", version, len(targets))

	distDir := "dist"
	buildDir := filepath.Join(distDir, ".build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return err
	}
	// 中间产物不留在 dist 里，否则用户下载目录里会混进一堆没用的二进制。
	defer os.RemoveAll(buildDir)

	var sums []string
	for _, t := range targets {
		archive, err := buildOne(t, version, buildDir)
		if err != nil {
			return err
		}
		sum, err := sha256File(archive)
		if err != nil {
			return err
		}
		sums = append(sums, fmt.Sprintf("%s  %s", sum, filepath.Base(archive)))
	}

	// checksums.txt 用「两个空格」分隔，这是 sha256sum -c 认识的格式。
	sort.Strings(sums)
	checkPath := filepath.Join(distDir, "checksums.txt")
	if err := os.WriteFile(checkPath, []byte(strings.Join(sums, "\n")+"\n"), 0o644); err != nil {
		return err
	}

	fmt.Printf("\n产物清单:\n")
	for _, s := range sums {
		fmt.Printf("  %s\n", s)
	}
	fmt.Printf("  %s（含全部 %d 个包的 sha256）\n", checkPath, len(sums))
	return nil
}

// buildOne 编译一个平台并打成压缩包，返回压缩包路径。
func buildOne(t target, version, buildDir string) (string, error) {
	name := fmt.Sprintf("minilang_%s_%s_%s", version, t.os, t.arch)
	binName := "minilang"
	if t.os == "windows" {
		binName += ".exe"
	}

	binPath := filepath.Join(buildDir, t.os+"_"+t.arch, binName)
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		return "", err
	}

	// CGO_ENABLED=0 是为了让产物是纯静态的：交叉编译时本来也没法用 cgo，
	// 显式关掉可以避免在装了 gcc 的机器上意外链接到 libc，
	// 那样打出来的 linux 包在 alpine 里会直接跑不起来。
	cmd := exec.Command("go", "build",
		"-trimpath",
		"-ldflags", "-s -w",
		"-o", binPath,
		"./cmd/minilang",
	)
	cmd.Env = append(os.Environ(),
		"GOOS="+t.os,
		"GOARCH="+t.arch,
		"CGO_ENABLED=0",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("编译 %s/%s 失败: %w", t.os, t.arch, err)
	}

	// 只有本机平台的二进制能立刻验证。交叉编译的产物只能等 CI 或用户去跑，
	// 所以本机这一份必须验 —— 否则「包能解开、但里面是个坏二进制」这种问题
	// 会一路漏到发布页面上。
	if t.os == runtime.GOOS && t.arch == runtime.GOARCH {
		if err := verifyBinary(binPath, version); err != nil {
			return "", err
		}
	}

	files, err := collectPayload(binPath, binName)
	if err != nil {
		return "", err
	}

	prefix := name + "/"
	var archivePath string
	if t.os == "windows" {
		archivePath = filepath.Join("dist", name+".zip")
		err = writeZip(archivePath, prefix, files)
	} else {
		archivePath = filepath.Join("dist", name+".tar.gz")
		err = writeTarGz(archivePath, prefix, files)
	}
	if err != nil {
		return "", err
	}

	size, err := fileSize(archivePath)
	if err != nil {
		return "", err
	}
	fmt.Printf("  %-42s %8s  %d 个文件\n", filepath.Base(archivePath), humanSize(size), len(files))
	return archivePath, nil
}

// collectPayload 决定压缩包里装什么。
//
// 刻意把 docs/ 和 examples/ 一起打进去：下载包的人拿到的是能直接跑示例的完整目录，
// 而不是一个孤零零的二进制 —— 后者还得回仓库翻示例文件才能验证它到底能不能用。
func collectPayload(binPath, binName string) ([]payload, error) {
	files := []payload{
		{name: binName, src: binPath, mode: 0o755},
		{name: "README.md", src: "README.md", mode: 0o644},
		{name: "LICENSE", src: "LICENSE", mode: 0o644},
	}

	for _, dir := range []string{"docs", "examples"} {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			// 只带 md 和 ml，避免把编辑器临时文件也打进去。
			ext := filepath.Ext(path)
			if ext != ".md" && ext != ".ml" {
				return nil
			}
			files = append(files, payload{
				name: filepath.ToSlash(path),
				src:  path,
				mode: 0o644,
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func writeZip(dst, prefix string, files []payload) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	now := time.Now()
	for _, p := range files {
		hdr := &zip.FileHeader{
			Name:     prefix + p.name,
			Method:   zip.Deflate,
			Modified: now,
		}
		hdr.SetMode(p.mode)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		if err := copyFile(w, p.src); err != nil {
			return err
		}
	}
	return zw.Close()
}

func writeTarGz(dst, prefix string, files []payload) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	now := time.Now()
	for _, p := range files {
		info, err := os.Stat(p.src)
		if err != nil {
			return err
		}
		hdr := &tar.Header{
			Name:     prefix + p.name,
			Mode:     int64(p.mode.Perm()),
			Size:     info.Size(),
			ModTime:  now,
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if err := copyFile(tw, p.src); err != nil {
			return err
		}
	}
	// 两层 Close 都要检查：tar 的收尾块和 gzip 的 CRC 都在 Close 时才写出去，
	// 吞掉这里的错误会得到一个「能解开但校验失败」的压缩包。
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func copyFile(w io.Writer, src string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

// verifyBinary 跑一下刚编出来的二进制，确认它至少能报出版本号。
func verifyBinary(path, version string) error {
	out, err := exec.Command(path, "version").Output()
	if err != nil {
		return fmt.Errorf("本机二进制无法运行: %w", err)
	}
	if !strings.Contains(string(out), version) {
		return fmt.Errorf("本机二进制版本不符: 期望 %s，实际输出 %q", version, strings.TrimSpace(string(out)))
	}
	return nil
}

// readVersion 从源码里读版本号，避免版本号在 Makefile / 打包脚本 / 源码里各写一份。
var versionRe = regexp.MustCompile(`(?m)^const version = "([^"]+)"`)

func readVersion() (string, error) {
	src, err := os.ReadFile(filepath.Join("cmd", "minilang", "main.go"))
	if err != nil {
		return "", err
	}
	m := versionRe.FindSubmatch(src)
	if m == nil {
		return "", fmt.Errorf("在 cmd/minilang/main.go 里找不到 const version = \"...\"")
	}
	return string(m[1]), nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// humanSize 把字节数格式化成 3 位有效宽度，便于对齐。
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%4d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 2; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

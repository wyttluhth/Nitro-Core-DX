package corelx

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// TestNcdxContainerCompiles builds a real .ncdx container (zip of main.corelx +
// a .cxasset) and confirms the compiler reads it, loads the image, and produces
// a ROM.
func TestNcdxContainerCompiles(t *testing.T) {
	asset, err := os.ReadFile(`/home/aj/Documents/Development/Nitro-Core-DX/Games/NitroPackInDemo/corelx/park_floor.cxasset`)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	main := `asset ParkFloor: image "park_floor.cxasset"
function Start()
    matrix_plane.enable(0, 32)
    matrix_plane.load_bitmap(ParkFloor, 0)
    while true
        wait_vblank()
`
	dir := t.TempDir()
	ncdx := filepath.Join(dir, "MyGame.ncdx")
	zf, err := os.Create(ncdx)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	for name, content := range map[string][]byte{
		"main.corelx":        []byte(main),
		"park_floor.cxasset": asset,
		"project.toml":       []byte("title = \"MyGame\"\n"),
	} {
		w, _ := zw.Create(name)
		w.Write(content)
	}
	zw.Close()
	zf.Close()

	out := filepath.Join(dir, "MyGame.cart")
	if _, err := CompileProject(ncdx, &CompileOptions{OutputPath: out}); err != nil {
		t.Fatalf("compile .ncdx: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil || info.Size() == 0 {
		t.Fatalf("expected a non-empty .cart, got %v (%v)", info, err)
	}
}

// TestOrphanAssetRejected: an unreferenced .cxasset in the project is an error.
func TestOrphanAssetRejected(t *testing.T) {
	asset, _ := os.ReadFile(`/home/aj/Documents/Development/Nitro-Core-DX/Games/NitroPackInDemo/corelx/park_floor.cxasset`)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "park_floor.cxasset"), asset, 0644)
	os.WriteFile(filepath.Join(dir, "main.corelx"), []byte("function Start()\n    while true\n        wait_vblank()\n"), 0644)
	_, err := CompileProject(filepath.Join(dir, "main.corelx"), &CompileOptions{OutputPath: filepath.Join(dir, "o.cart")})
	if err == nil {
		t.Fatal("expected orphan-asset error")
	}
}

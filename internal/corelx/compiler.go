package corelx

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"nitro-core-dx/internal/rom"
)

type CompileOptions struct {
	OutputPath            string
	ManifestOutputPath    string
	DiagnosticsOutputPath string
	BundleOutputPath      string
	AssetManifestPath     string
	EntryBank             uint8
	EntryOffset           uint16
	MaxROMBytes           uint32
	SectionBudgets        map[string]uint32
	EmitROMBytes          bool
	EmitManifestJSON      bool
	EmitDiagnosticsJSON   bool
	EmitBundleJSON        bool
}

type CompileResult struct {
	Program          *Program
	NormalizedAssets []AssetIR
	AssetSourceFiles []string
	ROMBytes         []byte
	Manifest         *BuildManifest
	ManifestJSON     []byte
	DiagnosticsJSON  []byte
	BundleJSON       []byte
	MemoryMap        []MemoryMapEntry
	MemoryMapText    []byte
	Diagnostics      []Diagnostic
}

func defaultCompileOptions() CompileOptions {
	return CompileOptions{
		EntryBank:           1,
		EntryOffset:         0x8000,
		EmitROMBytes:        true,
		EmitManifestJSON:    true,
		EmitDiagnosticsJSON: true,
		EmitBundleJSON:      true,
	}
}

// CompileProject is the production compiler entrypoint scaffold.
// Current implementation compiles a single CoreLX source file and returns structured diagnostics.
func CompileProject(sourcePath string, opts *CompileOptions) (*CompileResult, error) {
	// Resolve a .ncdx container or project directory to its main source file.
	mainPath, cleanup, openErr := openProject(sourcePath)
	if openErr != nil {
		diag := Diagnostic{
			Category: CategoryIOError,
			Code:     "E_IO_OPEN_PROJECT",
			Message:  openErr.Error(),
			File:     sourcePath,
			Severity: SeverityError,
			Stage:    StageIO,
		}
		return &CompileResult{Diagnostics: []Diagnostic{diag}}, &DiagnosticsError{Diagnostics: []Diagnostic{diag}}
	}
	defer cleanup()
	sourcePath = mainPath

	source, err := os.ReadFile(sourcePath)
	if err != nil {
		diag := Diagnostic{
			Category: CategoryIOError,
			Code:     "E_IO_READ_SOURCE",
			Message:  err.Error(),
			File:     sourcePath,
			Severity: SeverityError,
			Stage:    StageIO,
		}
		if diag.EndLine == 0 {
			diag.EndLine = diag.Line
		}
		if diag.EndColumn == 0 {
			diag.EndColumn = diag.Column
		}
		return &CompileResult{Diagnostics: []Diagnostic{diag}}, &DiagnosticsError{Diagnostics: []Diagnostic{diag}}
	}
	return CompileSource(string(source), sourcePath, opts)
}

// CompileSource compiles CoreLX source text with optional sourcePath metadata.
func CompileSource(source, sourcePath string, opts *CompileOptions) (result *CompileResult, err error) {
	cfg := defaultCompileOptions()
	if opts != nil {
		mergeCompileOptions(&cfg, *opts)
	}

	currentStage := StageIO
	defer func() {
		if r := recover(); r != nil {
			if result == nil {
				result = &CompileResult{}
			}
			diag := internalCompilerDiagnostic(currentStage, sourcePath, r)
			result.Diagnostics = append(result.Diagnostics, diag)
			err = &DiagnosticsError{Diagnostics: result.Diagnostics}
		}

		if result == nil {
			return
		}
		normalizeDiagnosticRanges(result.Diagnostics)
		if cfg.EmitDiagnosticsJSON || cfg.DiagnosticsOutputPath != "" {
			if b, mErr := json.MarshalIndent(result.Diagnostics, "", "  "); mErr == nil {
				result.DiagnosticsJSON = b
				if cfg.DiagnosticsOutputPath != "" {
					if wErr := os.WriteFile(cfg.DiagnosticsOutputPath, b, 0644); wErr != nil {
						result.Diagnostics = append(result.Diagnostics, Diagnostic{
							Category: CategoryIOError,
							Code:     "E_IO_WRITE_DIAGNOSTICS",
							Message:  wErr.Error(),
							File:     cfg.DiagnosticsOutputPath,
							Severity: SeverityError,
							Stage:    StageIO,
						})
						// refresh DiagnosticsJSON with the appended diagnostic if possible
						if b2, mErr2 := json.MarshalIndent(result.Diagnostics, "", "  "); mErr2 == nil {
							result.DiagnosticsJSON = b2
						}
						if err == nil {
							err = &DiagnosticsError{Diagnostics: result.Diagnostics}
						}
					}
				}
			}
		}
		if cfg.EmitBundleJSON || cfg.BundleOutputPath != "" {
			bundle := BuildCompileBundle(result)
			if b, mErr := json.MarshalIndent(bundle, "", "  "); mErr == nil {
				result.BundleJSON = b
				if cfg.BundleOutputPath != "" {
					if wErr := os.WriteFile(cfg.BundleOutputPath, b, 0644); wErr != nil {
						result.Diagnostics = append(result.Diagnostics, Diagnostic{
							Category: CategoryIOError,
							Code:     "E_IO_WRITE_BUNDLE",
							Message:  wErr.Error(),
							File:     cfg.BundleOutputPath,
							Severity: SeverityError,
							Stage:    StageIO,
						})
						// Refresh derived JSON outputs after appending the write error.
						if b2, err2 := json.MarshalIndent(result.Diagnostics, "", "  "); err2 == nil {
							result.DiagnosticsJSON = b2
						}
						if b3, err3 := json.MarshalIndent(BuildCompileBundle(result), "", "  "); err3 == nil {
							result.BundleJSON = b3
						}
						if err == nil {
							err = &DiagnosticsError{Diagnostics: result.Diagnostics}
						}
					}
				}
			}
		}
	}()

	result = &CompileResult{
		Diagnostics: make([]Diagnostic, 0),
	}

	lexer := NewLexer(source)
	currentStage = StageLexer
	tokens, err := lexer.Tokenize()
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Category: CategoryLexError,
			Code:     "E_LEX_TOKENIZE",
			Message:  err.Error(),
			File:     sourcePath,
			Severity: SeverityError,
			Stage:    StageLexer,
		})
		return result, &DiagnosticsError{Diagnostics: result.Diagnostics}
	}

	for _, tok := range tokens {
		if tok.Type == TOKEN_ERROR {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{
				Category: CategoryLexError,
				Code:     "E_LEX_TOKEN",
				Message:  tok.Literal,
				File:     sourcePath,
				Line:     tok.Line,
				Column:   tok.Column,
				Severity: SeverityError,
				Stage:    StageLexer,
			})
		}
	}
	if HasErrors(result.Diagnostics) {
		return result, &DiagnosticsError{Diagnostics: result.Diagnostics}
	}

	parser := NewParser(tokens)
	currentStage = StageParser
	program, err := parser.Parse()
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, parseDiagnostic(err, sourcePath))
		return result, &DiagnosticsError{Diagnostics: result.Diagnostics}
	}
	result.Program = program

	currentStage = StageAsset
	externalAssets, externalSources, externalDiags := loadProjectAssets(sourcePath, cfg)
	result.Diagnostics = append(result.Diagnostics, externalDiags...)
	if HasErrors(result.Diagnostics) {
		return result, &DiagnosticsError{Diagnostics: result.Diagnostics}
	}
	if len(externalAssets) > 0 {
		program.Assets = append(program.Assets, externalAssets...)
		result.AssetSourceFiles = append(result.AssetSourceFiles, externalSources...)
	}

	currentStage = StageSemantic
	semDiags := AnalyzeWithDiagnostics(program)
	stampDiagnosticsFile(semDiags, sourcePath)
	result.Diagnostics = append(result.Diagnostics, semDiags...)
	if HasErrors(result.Diagnostics) {
		return result, &DiagnosticsError{Diagnostics: result.Diagnostics}
	}

	currentStage = StageAsset
	assets, assetDiags := NormalizeAssets(program, sourcePath)
	result.NormalizedAssets = assets
	result.Diagnostics = append(result.Diagnostics, assetDiags...)
	if HasErrors(result.Diagnostics) {
		return result, &DiagnosticsError{Diagnostics: result.Diagnostics}
	}

	// Load external image (.cxasset) bitmaps, lay them out in the ROM data
	// region, and hand them to codegen for load_bitmap.
	imageAssets, imageRegion, imgErr := loadImageAssets(program, sourcePath)
	if imgErr != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Category: CategoryAssetParseError,
			Code:     "E_IMAGE_ASSET",
			Message:  imgErr.Error(),
			File:     sourcePath,
			Severity: SeverityError,
			Stage:    StageAsset,
		})
		return result, &DiagnosticsError{Diagnostics: result.Diagnostics}
	}

	builder := rom.NewROMBuilder()
	generator := NewCodeGenerator(program, builder)
	generator.SetNormalizedAssets(assets)
	generator.SetImageAssets(imageAssets)
	currentStage = StageCodegen
	if err := generator.Generate(); err != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Category: CategoryBackendCodegenError,
			Code:     "E_CODEGEN_GENERATE",
			Message:  err.Error(),
			File:     sourcePath,
			Severity: SeverityError,
			Stage:    StageCodegen,
		})
		return result, &DiagnosticsError{Diagnostics: result.Diagnostics}
	}

	if len(imageRegion) > 0 {
		builder.SetDataRegion(imageDataStartBank, imageRegion)
	}

	currentStage = StagePack
	codeBytes := uint32(builder.GetCodeLength() * 2)
	romBytes, err := builder.BuildROMBytes(cfg.EntryBank, cfg.EntryOffset)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Category: CategoryLayoutError,
			Code:     "E_PACK_BUILD_ROM",
			Message:  err.Error(),
			File:     sourcePath,
			Severity: SeverityError,
			Stage:    StagePack,
		})
		return result, &DiagnosticsError{Diagnostics: result.Diagnostics}
	}

	if cfg.EmitROMBytes {
		result.ROMBytes = romBytes
	}
	result.MemoryMap = generator.MemoryMap()
	result.MemoryMapText = formatMemoryMap(result.MemoryMap)
	if cfg.OutputPath != "" && len(result.MemoryMap) > 1 {
		// Listing emitted alongside the ROM for debugger/symbol use
		// (charter memory model: tooling-visible allocation).
		mapPath := cfg.OutputPath + ".memmap"
		if wErr := os.WriteFile(mapPath, result.MemoryMapText, 0644); wErr != nil {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{
				Category: CategoryIOError,
				Code:     "E_IO_WRITE_MEMMAP",
				Message:  wErr.Error(),
				File:     mapPath,
				Severity: SeverityWarning,
				Stage:    StageIO,
			})
		}
	}
	result.Manifest = buildManifestFromCompileState(sourcePath, cfg.EntryBank, cfg.EntryOffset, codeBytes, uint32(len(romBytes)), program, assets)
	if result.Manifest != nil && len(result.AssetSourceFiles) > 0 {
		result.Manifest.SourceFiles = uniqueStrings(append(result.Manifest.SourceFiles, result.AssetSourceFiles...))
	}
	currentStage = StagePack
	packDiags := validatePackBudgets(result.Manifest, cfg, sourcePath)
	result.Diagnostics = append(result.Diagnostics, packDiags...)
	if HasErrors(result.Diagnostics) {
		return result, &DiagnosticsError{Diagnostics: result.Diagnostics}
	}
	if result.Manifest != nil && (cfg.EmitManifestJSON || cfg.ManifestOutputPath != "") {
		manifestJSON, mErr := json.MarshalIndent(result.Manifest, "", "  ")
		if mErr != nil {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{
				Category: CategoryInternalCompilerError,
				Code:     "E_MANIFEST_MARSHAL",
				Message:  mErr.Error(),
				File:     sourcePath,
				Severity: SeverityError,
				Stage:    StagePack,
			})
			return result, &DiagnosticsError{Diagnostics: result.Diagnostics}
		}
		result.ManifestJSON = manifestJSON
		if cfg.ManifestOutputPath != "" {
			currentStage = StageIO
			if wErr := os.WriteFile(cfg.ManifestOutputPath, manifestJSON, 0644); wErr != nil {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{
					Category: CategoryIOError,
					Code:     "E_IO_WRITE_MANIFEST",
					Message:  wErr.Error(),
					File:     cfg.ManifestOutputPath,
					Severity: SeverityError,
					Stage:    StageIO,
				})
				return result, &DiagnosticsError{Diagnostics: result.Diagnostics}
			}
		}
	}

	if cfg.OutputPath != "" {
		currentStage = StageIO
		if err := os.WriteFile(cfg.OutputPath, romBytes, 0644); err != nil {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{
				Category: CategoryIOError,
				Code:     "E_IO_WRITE_ROM",
				Message:  err.Error(),
				File:     cfg.OutputPath,
				Severity: SeverityError,
				Stage:    StageIO,
			})
			return result, &DiagnosticsError{Diagnostics: result.Diagnostics}
		}
	}

	return result, nil
}

func mergeCompileOptions(dst *CompileOptions, src CompileOptions) {
	if src.OutputPath != "" {
		dst.OutputPath = src.OutputPath
	}
	if src.ManifestOutputPath != "" {
		dst.ManifestOutputPath = src.ManifestOutputPath
	}
	if src.DiagnosticsOutputPath != "" {
		dst.DiagnosticsOutputPath = src.DiagnosticsOutputPath
	}
	if src.BundleOutputPath != "" {
		dst.BundleOutputPath = src.BundleOutputPath
	}
	if src.AssetManifestPath != "" {
		dst.AssetManifestPath = src.AssetManifestPath
	}
	if src.EntryBank != 0 {
		dst.EntryBank = src.EntryBank
	}
	if src.EntryOffset != 0 {
		dst.EntryOffset = src.EntryOffset
	}
	if src.MaxROMBytes != 0 {
		dst.MaxROMBytes = src.MaxROMBytes
	}
	if src.SectionBudgets != nil {
		dst.SectionBudgets = src.SectionBudgets
	}
	// Booleans intentionally only override when true in this phase to preserve defaults for partial options.
	// If explicit disabling becomes necessary, switch to pointer-based options or a builder config.
	if src.EmitROMBytes {
		dst.EmitROMBytes = true
	}
	if src.EmitManifestJSON {
		dst.EmitManifestJSON = true
	}
	if src.EmitDiagnosticsJSON {
		dst.EmitDiagnosticsJSON = true
	}
	if src.EmitBundleJSON {
		dst.EmitBundleJSON = true
	}
}

func validatePackBudgets(manifest *BuildManifest, cfg CompileOptions, sourcePath string) []Diagnostic {
	if manifest == nil {
		return nil
	}
	diags := make([]Diagnostic, 0)
	if cfg.MaxROMBytes > 0 && manifest.PlannedROMSizeBytes > cfg.MaxROMBytes {
		diags = append(diags, Diagnostic{
			Category: CategoryOverflowError,
			Code:     "E_OVERFLOW_ROM",
			Message:  fmt.Sprintf("planned ROM size %d exceeds budget %d", manifest.PlannedROMSizeBytes, cfg.MaxROMBytes),
			File:     sourcePath,
			Severity: SeverityError,
			Stage:    StagePack,
			Notes: []string{
				"Reduce code/assets, or increase the ROM budget in CompileOptions.",
			},
		})
	}
	for _, s := range manifest.Sections {
		if cfg.SectionBudgets == nil {
			continue
		}
		budget, ok := cfg.SectionBudgets[s.Name]
		if !ok || budget == 0 {
			continue
		}
		if s.UsedBytes > budget {
			diags = append(diags, Diagnostic{
				Category: CategoryOverflowError,
				Code:     "E_OVERFLOW_SECTION",
				Message:  fmt.Sprintf("section %q uses %d bytes and exceeds budget %d", s.Name, s.UsedBytes, budget),
				File:     sourcePath,
				Severity: SeverityError,
				Stage:    StagePack,
				Notes: []string{
					"Adjust asset sizes or raise the section budget.",
				},
			})
		}
	}
	return diags
}

// CompileFile is a convenience wrapper for the current single-file workflow.
// It now uses the production compiler entrypoint and structured diagnostics internally.
func CompileFile(sourcePath, outputPath string) error {
	_, err := CompileProject(sourcePath, &CompileOptions{OutputPath: outputPath})
	return err
}

func stampDiagnosticsFile(diags []Diagnostic, file string) {
	if file == "" {
		return
	}
	for i := range diags {
		if diags[i].File == "" {
			diags[i].File = file
		}
		for j := range diags[i].Related {
			if diags[i].Related[j].File == "" {
				diags[i].Related[j].File = file
			}
		}
	}
}

var parseErrRe = regexp.MustCompile(`parse error at line ([0-9]+), column ([0-9]+): (.*)$`)

func parseDiagnostic(err error, file string) Diagnostic {
	msg := err.Error()
	diag := Diagnostic{
		Category: CategorySyntaxError,
		Code:     "E_PARSE",
		Message:  msg,
		File:     file,
		Severity: SeverityError,
		Stage:    StageParser,
	}
	m := parseErrRe.FindStringSubmatch(msg)
	if len(m) == 4 {
		line, _ := strconv.Atoi(m[1])
		col, _ := strconv.Atoi(m[2])
		diag.Line = line
		diag.Column = col
		diag.Message = strings.TrimSpace(m[3])
	}
	return diag
}

func internalCompilerDiagnostic(stage DiagnosticStage, file string, recovered any) Diagnostic {
	return Diagnostic{
		Category: CategoryInternalCompilerError,
		Code:     "E_INTERNAL_PANIC",
		Message:  fmt.Sprintf("internal compiler panic: %v", recovered),
		File:     file,
		Severity: SeverityError,
		Stage:    stage,
	}
}

func normalizeDiagnosticRanges(diags []Diagnostic) {
	for i := range diags {
		if diags[i].Line > 0 && diags[i].EndLine == 0 {
			diags[i].EndLine = diags[i].Line
		}
		if diags[i].Line > 0 && diags[i].Column > 0 && diags[i].EndColumn == 0 {
			diags[i].EndColumn = diags[i].Column
		}
	}
}

// formatMemoryMap renders the WRAM allocation listing emitted with each build.
func formatMemoryMap(entries []MemoryMapEntry) []byte {
	var b strings.Builder
	b.WriteString("# CoreLX WRAM memory map (name  address  size  kind)\n")
	b.WriteString("# user scratch region (never compiler-allocated): 0x7000-0x7FFF\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "%-24s 0x%04X  %4d  %s\n", e.Name, e.Address, e.Size, e.Kind)
	}
	return []byte(b.String())
}

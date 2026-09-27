param([ValidateSet('all','pc','wasm')][string]$Target='all')
$ErrorActionPreference='Stop'
$repoRoot=Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $repoRoot
$binPath=Join-Path $repoRoot 'bin'
$webPath=Join-Path $binPath 'web'
$nativeAssets=Join-Path $binPath 'assets'
$webAssets=Join-Path $webPath 'assets'
New-Item -ItemType Directory -Force -Path $binPath | Out-Null
if ($Target -in @('all','pc')) {
	foreach ($process in (Get-Process -Name redust -ErrorAction SilentlyContinue)) {
		Write-Host "Stopping ReDust process $($process.Id) before replacing bin/redust.exe"
		Stop-Process -Id $process.Id -Force
		Wait-Process -Id $process.Id -Timeout 10 -ErrorAction SilentlyContinue
	}
	$previousGoOS=$env:GOOS
	$previousGoARCH=$env:GOARCH
	try {
		$env:GOOS='windows'
		$env:GOARCH='amd64'
		& go build -o (Join-Path $binPath 'redust.exe') .
		if ($LASTEXITCODE -ne 0) { throw "Windows build failed with exit code $LASTEXITCODE" }
	} finally {
		if ($null -eq $previousGoOS) { Remove-Item Env:GOOS -ErrorAction SilentlyContinue } else { $env:GOOS=$previousGoOS }
		if ($null -eq $previousGoARCH) { Remove-Item Env:GOARCH -ErrorAction SilentlyContinue } else { $env:GOARCH=$previousGoARCH }
	}
	Write-Host 'Built PC executable: bin/redust.exe'
}
if ($Target -in @('all','wasm')) {
	New-Item -ItemType Directory -Force -Path $webPath,$webAssets | Out-Null
	foreach ($name in @('index.html','favicon.ico','favicon.png')) { Copy-Item -LiteralPath (Join-Path $PSScriptRoot "../web/$name") -Destination $webPath -Force }
	if (Test-Path -LiteralPath $nativeAssets) {
		foreach ($item in (Get-ChildItem -LiteralPath $nativeAssets -Force)) { Copy-Item -LiteralPath $item.FullName -Destination $webAssets -Recurse -Force }
	}
	$goRoot=(& go env GOROOT).Trim()
	$wasmExec=Join-Path $goRoot 'lib/wasm/wasm_exec.js'
	if (-not (Test-Path -LiteralPath $wasmExec)) { $wasmExec=Join-Path $goRoot 'misc/wasm/wasm_exec.js' }
	if (-not (Test-Path -LiteralPath $wasmExec)) { throw "wasm_exec.js was not found under $goRoot" }
	Copy-Item -LiteralPath $wasmExec -Destination (Join-Path $webPath 'wasm_exec.js') -Force
	$previousGoOS=$env:GOOS
	$previousGoARCH=$env:GOARCH
	try {
		$env:GOOS='js'
		$env:GOARCH='wasm'
		& go build -trimpath -ldflags='-s -w' -o (Join-Path $webPath 'redust.wasm') .
		if ($LASTEXITCODE -ne 0) { throw "WebAssembly build failed with exit code $LASTEXITCODE" }
	} finally {
		if ($null -eq $previousGoOS) { Remove-Item Env:GOOS -ErrorAction SilentlyContinue } else { $env:GOOS=$previousGoOS }
		if ($null -eq $previousGoARCH) { Remove-Item Env:GOARCH -ErrorAction SilentlyContinue } else { $env:GOARCH=$previousGoARCH }
	}
	$wasmFile=Join-Path $webPath 'redust.wasm'
	$compressedWasmFile=Join-Path $webPath 'redust.wasm.gz'
	$wasmStream=[System.IO.File]::OpenRead($wasmFile)
	$compressedStream=[System.IO.File]::Create($compressedWasmFile)
	try {
		$gzipStream=[System.IO.Compression.GZipStream]::new($compressedStream,[System.IO.Compression.CompressionLevel]::Optimal)
		try { $wasmStream.CopyTo($gzipStream) } finally { $gzipStream.Dispose() }
	} finally { $wasmStream.Dispose(); $compressedStream.Dispose() }
	Write-Host 'Built WebAssembly bundle: bin/web'
}

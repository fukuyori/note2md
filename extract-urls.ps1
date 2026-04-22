[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [string]$Month
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$inputPath = Join-Path -Path (Get-Location) -ChildPath "$Month.txt"
$outputPath = Join-Path -Path (Get-Location) -ChildPath "urls_$Month.txt"

if (-not (Test-Path -LiteralPath $inputPath -PathType Leaf)) {
    throw "Input file not found: $inputPath"
}

$content = Get-Content -LiteralPath $inputPath -Raw

$urls = (($content -replace "http", "`nhttp") -split "\r?\n") |
    ForEach-Object {
        if ($_.Length -gt 38) {
            $_.Substring(0, 38)
        }
        else {
            $_
        }
    } |
    Where-Object { $_ -match "fukuy/n" } |
    Sort-Object -Unique

Set-Content -LiteralPath $outputPath -Value $urls

Write-Output (Resolve-Path -LiteralPath $outputPath)

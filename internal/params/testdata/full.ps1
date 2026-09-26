<#
.SYNOPSIS
Rebuilds the search index for one site.
.DESCRIPTION
Reads the site manifest, walks the content tree, and writes a fresh index.
Safe to run repeatedly.
.PARAMETER Server
The server to rebuild.
.PARAMETER Count
How many documents to process.
.PARAMETER Mode
Which index to build.
.EXAMPLE
PS> .\full.ps1 -Server dc01 -Mode Full
Rebuilding the full index takes an hour.
.EXAMPLE
A quick partial run
PS> .\full.ps1 -Mode Partial
.NOTES
Requires the SqlServer module and a service account with write access.
.LINK
https://example.com/runbooks/index-rebuild
.LINK
https://example.com/runbooks/sql
#>
#Requires -Version 7.0
#Requires -Modules SqlServer
#Requires -RunAsAdministrator
[CmdletBinding()]
param(
    [Parameter(Mandatory, Position = 0)]
    [Alias('Host', 'Srv')]
    [ValidateSet('Full', 'Partial', 'Incremental')]
    [string]$Server = 'dc01',

    [Parameter(ValueFromPipeline)]
    [ValidateRange(1, 500)]
    [int]$Count = 50,

    [ValidateSet('Full', 'Partial')]
    [string]$Mode,

    [ValidatePattern('^[a-z][a-z0-9-]*$')]
    [string]$Tag,

    [ValidateNotNullOrEmpty()]
    [string]$OutDir = 'C:\index',

    [string[]]$Extras = @('a', 'b'),

    [switch]$Force,

    [pscredential]$Credential,

    $Generated = (Get-Date)
)
. "$PSScriptRoot/lib/common.ps1"
. (Join-Path $PSScriptRoot 'lib\helpers.ps1')
. ./sibling.ps1
. $shared
Write-Host $Server

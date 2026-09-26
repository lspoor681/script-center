[CmdletBinding()]
param(
    [string]$Server,
    [int]$Count
)
<#
.SYNOPSIS
A script whose help sits after the param block.
.DESCRIPTION
Get-Help ignores this block because it is not ahead of the param block.
.PARAMETER Server
The target server.
.NOTES
Recovered from the raw text instead.
#>
Write-Host $Server

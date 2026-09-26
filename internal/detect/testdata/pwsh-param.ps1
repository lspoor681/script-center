#Requires -Modules Az.Accounts
[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [string]$Server,

    [ValidateSet('Start', 'Stop')]
    [string]$Action = 'Start'
)
Write-Host "Connecting to $Server to $Action"

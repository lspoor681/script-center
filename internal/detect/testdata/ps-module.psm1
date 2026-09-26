function Get-Thing {
    [CmdletBinding()]
    param([string]$Name)
    Write-Output $Name
}

<#
.SYNOPSIS
Harvests parameter, help, and requirement metadata for PowerShell scripts.
.DESCRIPTION
Reads a JSON request {"paths": [...]} on stdin and writes a JSON array of
per-script records to stdout. Every script is wrapped in try/catch so one
malformed file cannot abort the batch.

The record has two sources and they are deliberately not mixed. The AST is
authoritative for the parameter schema: names, types, defaults, positions, and
validation attributes are all read from the parse tree, because Get-Help
synthesises values that the author never wrote (it reports a default of 0 for an
[int] parameter that declares none) and numbers positions differently from the
parser. Get-Help is used only for prose, which the AST does not carry at all.

Get-Help on a file returns a MAML-derived object whose useful members sit under
lowercase names that have no friendly alias: notes live in alertSet.alert, links
in relatedLinks.navigationLink, examples in examples.example, and per-parameter
prose in parameters.parameter.description. The friendly names Notes, Links and
Examples.Example do not exist on this object and silently return nothing.

Empty collections on that object report a count of one with empty members, so
every list is filtered on content rather than on Count.
#>
[CmdletBinding()]
param()

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

# The console host encodes stdout through the session codepage, and a character
# it cannot represent comes out as a raw byte instead of JSON text: U+2192 (the
# arrow in "A1B2C3 -> C3B2A1") becomes 0x1A on some PowerShell builds, which is
# a control character and fails the entire batch. UTF-8 can carry every
# character a script can contain, so both streams are pinned here. The BOM is
# suppressed because a byte 0xEF 0xBB 0xBF at the start would break JSON too.
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
[Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false)

# Read one JSON request from stdin. Reading to end is deliberate: stdin is the
# channel for the path list, so it cannot also carry the script.
$raw = [System.Console]::In.ReadToEnd()
if ([string]::IsNullOrWhiteSpace($raw)) {
  [Console]::Error.WriteLine('harvest: empty request on stdin')
  exit 2
}
$request = $raw | ConvertFrom-Json
$paths = @($request.paths)

# Returns a property value, or $null when the property is absent. The help object
# omits whole subtrees depending on which help tags the author used, so every
# access has to tolerate absence.
function Get-Prop {
  param($Object, [string]$Name)
  if ($null -eq $Object) { return $null }
  $prop = $Object.PSObject.Properties[$Name]
  if ($null -eq $prop) { return $null }
  return $prop.Value
}

# Returns a property value forced into an array. An absent property yields an
# empty array rather than a one-element array holding $null.
function Get-PropList {
  param($Object, [string]$Name)
  $value = Get-Prop $Object $Name
  if ($null -eq $value) { return @() }
  return @($value)
}

# Joins the .Text members of a MAML text list. A one-element list yields one
# string, a many-element list yields them joined by newlines.
function Join-Text {
  param($Object, [string]$Name)
  $items = @(Get-PropList $Object $Name | ForEach-Object { Get-Prop $_ 'Text' } |
    Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
  if ($items.Count -eq 0) { return '' }
  return ($items -join "`n").Trim()
}

# Returns the string a string-ish expression denotes, with quotes removed.
# A double-quoted string still containing variables comes back as its template,
# such as $PSScriptRoot/lib.ps1, so the caller can decide whether to resolve it.
function Get-StringValue {
  param($Expression)
  if ($null -eq $Expression) { return $null }
  switch ($Expression.GetType().Name) {
    'StringConstantExpressionAst' {
      try { return [string]$Expression.SafeGetValue() } catch { return $null }
    }
    'ExpandableStringExpressionAst' {
      return [string]$Expression.Value
    }
    'ConstantExpressionAst' {
      try { return [string]$Expression.SafeGetValue() } catch { return $null }
    }
    default { return $null }
  }
}

# Returns the literal value of an expression, or $null when the expression is
# not a literal. Used for validation attributes, whose arguments are always
# literals by the time the file parses.
function Get-LiteralValue {
  param($Expression)
  if ($null -eq $Expression) { return $null }
  $type = $Expression.GetType().Name
  if ($type -ne 'StringConstantExpressionAst' -and $type -ne 'ConstantExpressionAst') {
    return $null
  }
  try { return $Expression.SafeGetValue() } catch { return $null }
}

# Maps a .NET type name to a params.Kind string.
function Get-TypeKind {
  param([string]$TypeName)
  if ([string]::IsNullOrWhiteSpace($TypeName)) { return 'other' }
  $isArray = $TypeName.EndsWith('[]')
  $base = $TypeName
  if ($isArray) { $base = $TypeName.Substring(0, $TypeName.Length - 2) }
  # Each clause ends in an explicit break. A switch statement runs every clause
  # whose condition matches, so a type listed twice would otherwise yield a
  # two-element result rather than one kind.
  $kind = switch ($base) {
    'System.String' { 'string'; break }
    'System.Char' { 'string'; break }
    'System.Int16' { 'int'; break }
    'System.Int32' { 'int'; break }
    'System.Int64' { 'int'; break }
    'System.UInt16' { 'int'; break }
    'System.UInt32' { 'int'; break }
    'System.UInt64' { 'int'; break }
    'System.Byte' { 'int'; break }
    'System.SByte' { 'int'; break }
    'System.Double' { 'float'; break }
    'System.Single' { 'float'; break }
    'System.Decimal' { 'float'; break }
    'System.Boolean' { 'bool'; break }
    'System.Management.Automation.SwitchParameter' { 'bool'; break }
    'System.Management.Automation.PSCredential' { 'secret'; break }
    'System.Security.SecureString' { 'secret'; break }
    'System.IO.FileInfo' { 'path'; break }
    'System.IO.DirectoryInfo' { 'path'; break }
    default { 'other'; break }
  }
  if ($isArray) { return 'array' }
  return $kind
}

# Returns the elements of an array literal, or $null when the expression is not
# one. The parser wraps a literal in an array expression whose only statement is
# a command expression, so the literal is located by search rather than by
# walking a fixed shape that a future parser could change.
function Get-ArrayLiteral {
  param($Expression)
  if ($null -eq $Expression) { return $null }
  if ($Expression -is [System.Management.Automation.Language.ArrayLiteralAst]) {
    return $Expression
  }
  if ($Expression -is [System.Management.Automation.Language.ArrayExpressionAst]) {
    return $Expression.Find({
        param($node)
        $node -is [System.Management.Automation.Language.ArrayLiteralAst]
      }, $false)
  }
  return $null
}

# Returns a comma-joined display form of an array literal, or $null when any
# element is not a literal.
function Get-ArrayDisplay {
  param($Expression)
  $literal = Get-ArrayLiteral $Expression
  if ($null -eq $literal) { return $null }
  $values = @()
  foreach ($element in $literal.Elements) {
    $value = Get-StringValue $element
    if ($null -eq $value) { return $null }
    $values += $value
  }
  if ($values.Count -eq 0) { return $null }
  return ($values -join ', ')
}

# Reads a parameter's default. Literals are reported with both their source text
# and a flattened display form; anything that is not a literal is reported as an
# expression and is deliberately not evaluated, because running it here would
# have side effects and would not reflect what the script does at run time.
function Get-DefaultInfo {
  param($Expression)
  if ($null -eq $Expression) { return $null }
  $source = $Expression.Extent.Text
  $type = $Expression.GetType().Name

  $literal = $null
  switch ($type) {
    'StringConstantExpressionAst' { $literal = Get-StringValue $Expression; break }
    'ConstantExpressionAst' { $literal = Get-LiteralValue $Expression; break }
    'ExpandableStringExpressionAst' {
      $template = [string]$Expression.Value
      if ($template -notmatch '\$') { $literal = $template }
      break
    }
    { $_ -in 'ArrayLiteralAst', 'ArrayExpressionAst' } {
      # @('a','b') is a literal rather than an expression, and it is one of the
      # most common defaults worth showing pre-filled.
      $display = Get-ArrayDisplay $Expression
      if ($null -ne $display) {
        return [ordered]@{
          source = $source
          display = $display
          isExpression = $false
        }
      }
      break
    }
    default { break }
  }

  if ($null -ne $literal) {
    return [ordered]@{
      source = $source
      display = [string]$literal
      isExpression = $false
    }
  }
  return [ordered]@{
    source = $source
    display = ''
    isExpression = $true
  }
}

# Reads a Parameter attribute into a mutable shape the caller merges into.
function Read-ParameterAttribute {
  param($Attribute)
  $result = @{
    mandatory = $false
    position = -1
    fromPipeline = $false
    fromPipelineByName = $false
    remaining = $false
  }
  foreach ($named in @($Attribute.NamedArguments)) {
    $name = [string]$named.ArgumentName
    $value = Get-LiteralValue $named.Argument
    # A switch used as a bare name, as in [Parameter(Mandatory)], arrives with a
    # constant true rather than a missing value.
    if ($null -eq $value -and $null -ne $named.Argument) { $value = $true }
    switch ($name) {
      'Mandatory' { $result.mandatory = [bool]$value }
      'Position' { $result.position = [int]$value }
      'ValueFromPipeline' { $result.fromPipeline = [bool]$value }
      'ValueFromPipelineByPropertyName' { $result.fromPipelineByName = [bool]$value }
      'ValueFromRemainingArguments' { $result.remaining = [bool]$value }
      default { }
    }
  }
  return $result
}

# Reads a validation attribute into a constraint, or $null when the attribute
# has no counterpart in the constraint model.
function Read-Constraint {
  param($Attribute)
  $name = [string]$Attribute.TypeName.Name
  $positional = @($Attribute.PositionalArguments)
  switch ($name) {
    'ValidateSet' {
      $values = @()
      foreach ($arg in $positional) {
        $value = Get-StringValue $arg
        if ($null -ne $value) { $values += $value }
      }
      if ($values.Count -eq 0) { return $null }
      return [ordered]@{
        kind = 'set'
        label = 'one of: ' + ($values -join ', ')
        values = @($values)
        pattern = ''
        hasMin = $false
        hasMax = $false
      }
    }
    'ValidateRange' {
      if ($positional.Count -lt 2) { return $null }
      $low = Get-LiteralValue $positional[0]
      $high = Get-LiteralValue $positional[1]
      if ($null -eq $low -or $null -eq $high) { return $null }
      return [ordered]@{
        kind = 'range'
        label = 'between ' + $low + ' and ' + $high
        values = @()
        pattern = ''
        hasMin = $true
        hasMax = $true
        min = [double]$low
        max = [double]$high
        includeMin = $true
        includeMax = $true
      }
    }
    'ValidatePattern' {
      if ($positional.Count -lt 1) { return $null }
      $pattern = Get-StringValue $positional[0]
      if ($null -eq $pattern) { return $null }
      return [ordered]@{
        kind = 'pattern'
        label = 'must match ' + $pattern
        values = @()
        pattern = $pattern
        hasMin = $false
        hasMax = $false
      }
    }
    'ValidateLength' {
      if ($positional.Count -lt 2) { return $null }
      $low = Get-LiteralValue $positional[0]
      $high = Get-LiteralValue $positional[1]
      if ($null -eq $low -or $null -eq $high) { return $null }
      return [ordered]@{
        kind = 'length'
        label = 'length between ' + $low + ' and ' + $high
        values = @()
        pattern = ''
        hasMin = $true
        hasMax = $true
        min = [double]$low
        max = [double]$high
        includeMin = $true
        includeMax = $true
      }
    }
    'ValidateNotNullOrEmpty' {
      return [ordered]@{
        kind = 'notEmpty'
        label = 'must not be empty'
        values = @()
        pattern = ''
        hasMin = $false
        hasMax = $false
      }
    }
    'ValidateScript' {
      # The script block is not shown: it is arbitrary code, and rendering it as
      # a rule the user can read would overstate how much we understand.
      return [ordered]@{
        kind = 'notEmpty'
        label = 'must pass a custom check'
        values = @()
        pattern = ''
        hasMin = $false
        hasMax = $false
      }
    }
    default { return $null }
  }
}

# Renders a path fragment as text, keeping a variable in place as written. This
# differs from Get-StringValue, which reports only true literals: a path built
# from $PSScriptRoot is a path we can still reason about, because the importing
# script's directory is known when the dot-source is resolved.
function Get-PathText {
  param($Element)
  if ($null -eq $Element) { return $null }
  $text = Get-StringValue $Element
  if ($null -ne $text) { return $text }
  if ($Element -is [System.Management.Automation.Language.VariableExpressionAst]) {
    return [string]$Element.Extent.Text
  }
  return $null
}

# Returns the path a dot-source names, as written. A literal or interpolated
# string comes back as its text, with $PSScriptRoot and other variables left in
# place for the caller to resolve against the importing script's directory. A
# value the parser cannot reduce to text, such as a bare variable, comes back as
# the source that computed it, which is reported as unresolved rather than
# silently dropped.
function Get-DotSourceTarget {
  param($Element)
  if ($null -eq $Element) { return $null }

  $text = Get-StringValue $Element
  if ($null -ne $text) { return $text }

  # Join-Path $PSScriptRoot 'lib/common.ps1' is idiomatic in scripts that have to
  # work across Windows and Unix, so it is reduced here rather than reported as
  # unresolvable. Only literal arguments are folded; anything computed stays as
  # written.
  if ($Element -is [System.Management.Automation.Language.ParenExpressionAst]) {
    $inner = $Element.Find({
        param($node)
        $node -is [System.Management.Automation.Language.CommandAst]
      }, $false)
    if ($null -ne $inner -and $inner.GetCommandName() -eq 'Join-Path') {
      $parts = @()
      for ($i = 1; $i -lt $inner.CommandElements.Count; $i++) {
        $part = Get-PathText $inner.CommandElements[$i]
        if ($null -eq $part) { $parts = @(); break }
        $parts += $part
      }
      if ($parts.Count -gt 0) { return ($parts -join '/') }
    }
  }

  return [string]$Element.Extent.Text
}

# Builds the parameter, help, and requirement record for one script.
function Get-ScriptRecord {
  param([string]$Path)

  $record = [ordered]@{
    path = $Path
    error = $null
    params = @()
    help = [ordered]@{
      source = 'none'
      synopsis = ''
      description = ''
      notes = ''
      links = @()
      examples = @()
      params = @{}
      inputTypes = ''
      outputTypes = ''
    }
    requirements = [ordered]@{
      modules = @()
      runAsAdministrator = $false
      psVersion = ''
      psEditions = @()
    }
    dotSources = @()
    unparsedHelpBlock = $false
  }

  $tokens = $null
  $parseErrors = $null
  $ast = [System.Management.Automation.Language.Parser]::ParseFile(
    $Path, [ref]$tokens, [ref]$parseErrors)

  if ($null -eq $ast) {
    $record.error = 'the file could not be parsed'
    return $record
  }

  # --- requirements ---------------------------------------------------------
  $requirements = $ast.ScriptRequirements
  if ($null -ne $requirements) {
    $modules = @()
    foreach ($module in @($requirements.RequiredModules)) {
      $moduleName = [string]$module.Name
      if ([string]::IsNullOrWhiteSpace($moduleName)) { continue }
      $versionValue = $module.Version
      $versionText = ''
      if ($null -ne $versionValue) { $versionText = [string]$versionValue }
      $modules += [ordered]@{ name = $moduleName; version = $versionText }
    }
    $editions = @()
    foreach ($edition in @($requirements.RequiredPSEditions)) {
      $text = [string]$edition
      if (-not [string]::IsNullOrWhiteSpace($text)) { $editions += $text }
    }
    $versionValue = $requirements.RequiredPSVersion
    $versionText = ''
    if ($null -ne $versionValue) { $versionText = [string]$versionValue }

    $record.requirements = [ordered]@{
      modules = @($modules)
      runAsAdministrator = [bool]$requirements.IsElevationRequired
      psVersion = $versionText
      psEditions = @($editions)
    }
  }

  # --- dot sources ----------------------------------------------------------
  $dotSources = @()
  $commands = $ast.FindAll({
      param($node)
      $node -is [System.Management.Automation.Language.CommandAst]
    }, $true)
  foreach ($command in @($commands)) {
    # A dot-source is not identified by its command name. In ". $file" the parser
    # drops the dot from the element list entirely, so GetCommandName returns an
    # empty string, and in ". ./file.ps1" it returns the path because the single
    # element is read as a command name. The dot is only reliably visible in the
    # source text, where the operator is always a leading dot followed by
    # whitespace. A member access such as .ToString() is a different node type
    # and never reaches here.
    if ($command.Extent.Text -notmatch '^\s*\.\s+') { continue }
    $elements = @($command.CommandElements)
    if ($elements.Count -lt 1) { continue }
    $text = Get-DotSourceTarget $elements[0]
    if (-not [string]::IsNullOrWhiteSpace($text)) { $dotSources += $text }
  }
  $record.dotSources = @($dotSources)

  # --- parameters -----------------------------------------------------------
  $paramBlock = $ast.ParamBlock
  $harvested = @()
  if ($null -ne $paramBlock) {
    foreach ($declaration in @($paramBlock.Parameters)) {
      $name = [string]$declaration.Name.VariablePath.UserPath
      if ([string]::IsNullOrWhiteSpace($name)) { continue }

      $staticType = $declaration.StaticType
      $typeName = ''
      if ($null -ne $staticType) { $typeName = [string]$staticType.FullName }
      $kind = Get-TypeKind $typeName

      $aliases = @()
      $constraints = @()
      $mandatory = $false
      $position = -1
      $fromPipeline = $false

      # A declaration's Attributes collection mixes two unrelated node types: the
      # type constraints, which are [string] and [switch], and the real
      # attributes, which are [Parameter()] and [ValidateSet()]. A type
      # constraint carries a TypeName but no argument lists, so it is skipped
      # here. StaticType already accounts for it.
      foreach ($node in @($declaration.Attributes)) {
        if ($node -is [System.Management.Automation.Language.TypeConstraintAst]) {
          continue
        }
        switch ([string]$node.TypeName.Name) {
          'Parameter' {
            $read = Read-ParameterAttribute $node
            $mandatory = $read.mandatory
            if ($read.position -ge 0) { $position = $read.position }
            if ($read.fromPipeline -or $read.fromPipelineByName) { $fromPipeline = $true }
            break
          }
          'Alias' {
            foreach ($argument in @($node.PositionalArguments)) {
              $alias = Get-StringValue $argument
              if (-not [string]::IsNullOrWhiteSpace($alias)) { $aliases += $alias }
            }
            break
          }
          default {
            $constraint = Read-Constraint $node
            if ($null -ne $constraint) { $constraints += $constraint }
            break
          }
        }
      }

      # A closed set of values implies a dropdown regardless of declared type.
      $hasSet = $false
      foreach ($constraint in @($constraints)) {
        if ($constraint['kind'] -eq 'set') { $hasSet = $true; break }
      }
      if ($hasSet -and $kind -ne 'array') { $kind = 'enum' }
      if ($hasSet -and $kind -eq 'array') { $kind = 'array' }

      # Fallback: infer path kind from parameter name suffix when type is
      # ambiguous (string/other). This catches common naming conventions like
      # ConfigPath, InputFile, LogDir, OutputFolder, DataCsv, etc.
      if ($kind -in @('string', 'other')) {
        if ($name -match '(?i)(Path|File|Dir|Folder|Input|Csv|Txt|Output)$') {
          $kind = 'path'
        }
      }

      $harvested += [ordered]@{
        name = $name
        aliases = @($aliases)
        kind = $kind
        typeName = $typeName
        required = $mandatory
        # The parser numbers positions from zero and uses -1 for "none". The
        # form numbers them from one, so the shift happens here.
        position = $(if ($position -ge 0) { $position + 1 } else { 0 })
        fromPipeline = $fromPipeline
        default = (Get-DefaultInfo $declaration.DefaultValue)
        help = ''
        constraints = @($constraints)
      }
    }
  }
  $record.params = @($harvested)

  # --- help -----------------------------------------------------------------
  $help = Get-Help -Name $Path -ErrorAction SilentlyContinue
  $synopsis = [string](Get-Prop $help 'Synopsis')
  $description = Join-Text $help 'description'
  $notes = Join-Text (Get-Prop $help 'alertSet') 'alert'
  $inputTypes = Join-Text $help 'inputTypes'
  $outputTypes = Join-Text $help 'returnValues'

  # Links are gathered from every shape the MAML schema allows, in order, with
  # duplicates dropped. PowerShell is inconsistent about which member a .LINK tag
  # lands in: a second consecutive .LINK in the same block is reported as
  # linkText rather than navigationLink, and its text then carries on into
  # whatever followed the help block. A link is a URI, so a linkText blob
  # contributes only its first line and only when that line really is one.
  $links = @()
  $seenLinks = @{}
  $related = Get-Prop $help 'relatedLinks'
  $linkCandidates = @()
  foreach ($link in @(Get-PropList $related 'navigationLink')) {
    $linkCandidates += [string](Get-Prop $link 'uri')
    $linkCandidates += [string](Get-Prop $link 'linkText')
  }
  foreach ($link in @(Get-PropList $related 'text')) {
    $linkCandidates += [string](Get-Prop $link 'uri')
  }
  foreach ($link in @(Get-PropList $related 'linkText')) {
    $linkCandidates += [string](Get-Prop $link 'Text')
  }
  foreach ($candidate in $linkCandidates) {
    if ([string]::IsNullOrWhiteSpace($candidate)) { continue }
    $firstLine = ($candidate -split "`r?`n")[0].Trim()
    if ([string]::IsNullOrWhiteSpace($firstLine)) { continue }
    $parsed = $null
    if (-not [uri]::TryCreate($firstLine, [System.UriKind]::Absolute, [ref]$parsed)) { continue }
    if ($parsed.Scheme -notin @('http', 'https', 'ftp', 'ftps', 'mailto')) { continue }
    if ($seenLinks.ContainsKey($firstLine)) { continue }
    $seenLinks[$firstLine] = $true
    $links += $firstLine
  }

  $examples = @()
  foreach ($example in @(Get-PropList (Get-Prop $help 'examples') 'example')) {
    $code = [string](Get-Prop $example 'code')
    if ([string]::IsNullOrWhiteSpace($code)) { continue }
    $title = [string](Get-Prop $example 'title')
    # Get-Help always synthesises a banner title. It is presentation, not
    # something the author wrote, so it is dropped.
    if ($title -match '^\s*-{2,}\s*EXAMPLE') { $title = '' }
    $remarks = Join-Text $example 'remarks'
    $introduction = Join-Text $example 'introduction'
    $examples += [ordered]@{
      title = $title.Trim()
      # Get-Help merges an example's own heading and trailing prose into code
      # with no way to separate them. It is kept verbatim rather than guessed
      # at, which matters because examples are shown read-only.
      command = $code.Trim()
      remarks = (@($introduction, $remarks) | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }) -join "`n"
    }
  }

  $helpParams = @{}
  foreach ($parameter in @(Get-PropList (Get-Prop $help 'parameters') 'parameter')) {
    $parameterName = [string](Get-Prop $parameter 'name')
    if ([string]::IsNullOrWhiteSpace($parameterName)) { continue }
    $text = Join-Text $parameter 'description'
    if (-not [string]::IsNullOrWhiteSpace($text)) { $helpParams[$parameterName] = $text }
  }

  # Look for a comment-based help block in the raw text. Get-Help attaches a
  # comment-based help block to a script only when the block is the first or the
  # last thing in the file. It is not the middle: a block sitting between the
  # param block and a statement that follows it is silently ignored, and plenty
  # of scripts are written that way. A line comment ahead of a leading block
  # breaks the first case too, which is why this looks at the file as a whole
  # rather than at the help the toolchain happened to return.
  $hasHelpBlock = $false
  try {
    $text = [System.IO.File]::ReadAllText($Path)
    $match = [regex]::Match($text, '(?s)<#.*?#>')
    if ($match.Success) {
      $hasHelpBlock = $match.Value -match '(?im)^\s*\.\s*SYNOPSIS'
    }
  } catch {
    # An unreadable file is already reported by the parse above; this probe is
    # best effort only.
  }

  # Classify the help by what actually came back rather than by whether a
  # synopsis is present. Get-Help invents a synopsis out of the parameter list
  # when a script has no comment help at all, and that usage signature is not
  # something the author wrote, so it must not be presented as their summary.
  $gotProse = (-not [string]::IsNullOrWhiteSpace($description)) -or
    (-not [string]::IsNullOrWhiteSpace($notes)) -or
    ($helpParams.Count -gt 0) -or
    ($examples.Count -gt 0) -or
    ($links.Count -gt 0)

  $source = 'none'
  $unparsed = $false
  if ($gotProse) {
    $source = 'comment'
  } elseif ($hasHelpBlock) {
    # The block is there and the toolchain skipped it. The prose is recovered
    # from the raw text instead, so the synthesised synopsis is discarded rather
    # than passed off as a summary.
    $source = 'comment'
    $unparsed = $true
    $synopsis = ''
  } elseif (-not [string]::IsNullOrWhiteSpace($synopsis)) {
    $source = 'generated'
  }

  $record.help = [ordered]@{
    source = $source
    synopsis = $synopsis.Trim()
    description = $description
    notes = $notes
    links = @($links)
    examples = @($examples)
    params = $helpParams
    inputTypes = $inputTypes
    outputTypes = $outputTypes
  }
  $record.unparsedHelpBlock = $unparsed

  # Merge parameter prose onto the schema, which is the only reason Get-Help was
  # called. Its synthesised values are ignored.
  foreach ($parameter in $harvested) {
    $text = ''
    if ($helpParams.ContainsKey($parameter['name'])) { $text = $helpParams[$parameter['name']] }
    if ([string]::IsNullOrWhiteSpace($text)) {
      foreach ($alias in @($parameter['aliases'])) {
        if ($helpParams.ContainsKey($alias)) {
          $text = $helpParams[$alias]
          break
        }
      }
    }
    $parameter['help'] = $text
  }

  if ($parseErrors -and @($parseErrors).Count -gt 0) {
    $first = @($parseErrors)[0]
    $record.error = 'parse error: ' + [string]$first.Message
  }

  return $record
}

$records = @()
foreach ($path in $paths) {
  if ([string]::IsNullOrWhiteSpace($path)) { continue }
  try {
    $records += Get-ScriptRecord -Path $path
  } catch {
    $records += [ordered]@{
      path = $path
      error = $_.Exception.Message
      params = @()
      help = $null
      requirements = $null
      dotSources = @()
      unparsedHelpBlock = $false
    }
  }
}

# -InputObject keeps a single record as a one-element array instead of letting
# the pipeline collapse it to a bare object.
ConvertTo-Json -InputObject @($records) -Depth 12 -Compress

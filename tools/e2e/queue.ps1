param(
    [Parameter(Mandatory)]
    [ValidateSet('MuseSingle', 'MuseBatch', 'MuseVideo', 'FlowPause')]
    [string]$Scenario,
    [string]$ProcessName = 'SnakeMedia-dev',
    [string]$DataDir = (Join-Path $env:APPDATA 'SnakeMedia-dev'),
    [int]$TimeoutSeconds = 600
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
Add-Type -AssemblyName System.Windows.Forms
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class SnakeMediaE2EWindow {
    [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr window, int command);
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr window);
}
'@

function Wait-For([scriptblock]$Check, [int]$Seconds = 12, [string]$Description = 'application state') {
    $until = [DateTime]::UtcNow.AddSeconds($Seconds)
    do {
        if ($result = & $Check) { return $result }
        Start-Sleep -Milliseconds 200
    } while ([DateTime]::UtcNow -lt $until)
    throw "Timed out waiting for $Description."
}

$app = Wait-For {
    Get-Process -Name $ProcessName -ErrorAction SilentlyContinue |
        Where-Object { $_.MainWindowHandle -ne [IntPtr]::Zero } | Select-Object -First 1
}
[void][SnakeMediaE2EWindow]::ShowWindow($app.MainWindowHandle, 9)
[void][SnakeMediaE2EWindow]::SetForegroundWindow($app.MainWindowHandle)

function Find-Control([string]$Name, [string]$Type) {
    $root = [Windows.Automation.AutomationElement]::FromHandle($app.MainWindowHandle)
    $root.FindAll([Windows.Automation.TreeScope]::Descendants, [Windows.Automation.Condition]::TrueCondition) |
        Where-Object { $_.Current.Name -like $Name -and $_.Current.ControlType.ProgrammaticName -eq "ControlType.$Type" } |
        Select-Object -First 1
}

function Click-Control([string]$Name, [string]$Type = 'Button') {
    $control = Wait-For {
        $candidate = Find-Control $Name $Type
        if ($candidate -and $candidate.Current.IsEnabled) { $candidate }
    } -Description "$Type $Name"
    $pattern = $null
    if ($control.TryGetCurrentPattern([Windows.Automation.InvokePattern]::Pattern, [ref]$pattern)) {
        $pattern.Invoke()
    } elseif ($control.TryGetCurrentPattern([Windows.Automation.SelectionItemPattern]::Pattern, [ref]$pattern)) {
        $pattern.Select()
    } else {
        $control.SetFocus()
        [Windows.Forms.SendKeys]::SendWait('{ENTER}')
    }
}

function Dismiss-PreviousRun {
    if (Find-Control 'Mở lại phiên trước?' 'Window') {
        Click-Control 'Không'
    }
}

function Set-Prompt([string]$Name, [string]$Text, [switch]$Submit) {
    Dismiss-PreviousRun
    $box = Wait-For { Find-Control $Name 'Edit' }
    $value = $box.GetCurrentPattern([Windows.Automation.ValuePattern]::Pattern)
    if ($value.Current.Value -and $value.Current.Value -notlike '*SnakeMedia E2E*') {
        throw 'The prompt input contains an existing draft.'
    }
    $value.SetValue($Text)
    if ($Submit) {
        $null = Wait-For { Find-Control 'Prompt [1-9]* prompt' 'Edit' } -Description 'parsed prompts'
        Dismiss-PreviousRun
        $box.SetFocus()
        Start-Sleep -Milliseconds 250
        [Windows.Forms.SendKeys]::SendWait('{ENTER}')
    }
}

function Read-Job([string]$Path, [string]$Prompt) {
    if (Test-Path -LiteralPath $Path) {
        (Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json).records |
            Where-Object { $_.prompt -eq $Prompt }
    }
}

$tag = "SnakeMedia E2E $([guid]::NewGuid().ToString('N'))"
$prompt = "Create a flat illustration of one yellow teacup on a blue background, no letters. $tag"

if ($Scenario -eq 'FlowPause') {
    $journal = Join-Path $DataDir 'flow/image-queue.json'
    Click-Control 'Flow' 'Hyperlink'
    Click-Control 'Ảnh' 'Hyperlink'
    Click-Control 'Hàng loạt' 'TabItem'
    $config = Get-Content -LiteralPath (Join-Path $DataDir 'config.json') -Raw | ConvertFrom-Json
    if ($config.flow.queueThreads -ne 1 -or (Find-Control 'Chạy * prompt' 'Button')) {
        throw 'FlowPause needs one Flow queue thread and zero held prompts before the test.'
    }
    $second = "Create a flat illustration of a green pear, no letters. $tag"
    Set-Prompt 'Prompt * prompt' "$prompt`n$second" -Submit
    Click-Control 'Chạy 2 prompt'
    Click-Control 'Dừng'
    $held = Wait-For { $job = Read-Job $journal $second; if ($job.held -and $job.paused) { $job } }
    if ($held.requestId -or $held.status -ne 'queued') { throw 'The held job was already sent.' }
    $first = Wait-For { $job = Read-Job $journal $prompt; if ($job.status -in @('succeeded', 'failed', 'cancelled')) { $job } } $TimeoutSeconds
    [pscustomobject]@{ Scenario = $Scenario; FirstJob = $first.id; FirstStatus = $first.status; HeldJob = $held.id; HeldBeforeSend = $true }
    if ($first.status -ne 'succeeded') { throw 'Pause passed but the Flow service did not produce a result.' }
    return
}

$journal = Join-Path $DataDir 'muse/jobs.json'
Click-Control 'Muse' 'Hyperlink'
if ($Scenario -eq 'MuseVideo') {
    Click-Control 'Video' 'Hyperlink'
    $prompt = "Create a short silent landscape video of a paper pinwheel turning on a wooden table, fixed camera, no text. $tag"
} else {
    Click-Control 'Ảnh' 'Hyperlink'
}
if ($Scenario -ne 'MuseBatch') {
    Click-Control 'Đơn' 'TabItem'
    Click-Control 'x1'
    Set-Prompt '*Enter để tạo*' $prompt
    Dismiss-PreviousRun
    Click-Control 'Tạo*'
} else {
    Click-Control 'Hàng loạt' 'TabItem'
    if (Find-Control 'Chạy * prompt' 'Button') { throw 'MuseBatch needs zero held prompts before the test.' }
    Set-Prompt 'Prompt * prompt' $prompt -Submit
    $held = Wait-For { $job = Read-Job $journal $prompt; if ($job.held) { $job } }
    if ($held.sessionId) { throw 'The held Muse job was already sent.' }
    Click-Control 'Chạy 1 prompt'
}

$result = Wait-For { $job = Read-Job $journal $prompt; if ($job.status -in @('succeeded', 'failed', 'cancelled')) { $job } } $TimeoutSeconds
if ($result.status -ne 'succeeded' -or !$result.sessionId -or !$result.files.Count) {
    throw "Muse job $($result.id) ended as $($result.status) without a confirmed result."
}
Click-Control 'Thư viện' 'Hyperlink'
$null = Wait-For { Find-Control "Phóng to*$tag*" 'Button' }
if ($Scenario -eq 'MuseBatch') {
    foreach ($path in $result.savedFiles) {
        if (!(Test-Path -LiteralPath $path -PathType Leaf)) { throw 'A saved output file was missing.' }
    }
    if (!$result.savedFiles.Count) { throw 'The Muse batch did not save its result.' }
}
[pscustomobject]@{ Scenario = $Scenario; Job = $result.id; Status = $result.status; Files = $result.files.Count; Saved = $result.savedFiles.Count; VisibleInLibrary = $true }

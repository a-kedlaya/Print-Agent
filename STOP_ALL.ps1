#!/usr/bin/env powershell
# Stop All Components Script

Write-Host "`n=== STOPPING PALATE PRINT AGENT ===" -ForegroundColor Yellow

# Stop all background jobs
Write-Host "Stopping all background jobs..." -ForegroundColor Gray
Get-Job | Stop-Job
Get-Job | Remove-Job

Write-Host "* All components stopped" -ForegroundColor Green
Write-Host "`nSystem shutdown complete.`n" -ForegroundColor Gray

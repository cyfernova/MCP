Add-Type -AssemblyName System.Net.Http
$handler = [System.Net.Http.HttpClientHandler]::new()
$cert = Get-ChildItem -Path 'Cert:\CurrentUser\My' | Where-Object { $_.Thumbprint -eq 'A8AC0611C60677CA8569C4E8BE98B8B42B4E8DD3' } | Select-Object -First 1
$handler.ClientCertificates.Add($cert)
$client = [System.Net.Http.HttpClient]::new($handler)
$request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Post, 'https://localhost:9090/mcp/tools/list')
$request.Content = [System.Net.Http.StringContent]::new('{}', [System.Text.Encoding]::UTF8, 'application/json')
try {
    $response = $client.SendAsync($request).Result
    Write-Host "Status: $($response.StatusCode)"
    Write-Host "Content: $($response.Content.ReadAsStringAsync().Result)"
} catch {
    Write-Host "Error: $($_.Exception.InnerException.Message)"
}
$ErrorActionPreference='Stop'
# Storage pools and dynamic disks are not all exposed by Get-Disk.
# Match the physical serial, then verify it again through the opened device.
Add-Type @'
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;
using System.Text;
using Microsoft.Win32.SafeHandles;
public static class MigratekitConvertedDisk {
 [DllImport("kernel32.dll",CharSet=CharSet.Unicode,SetLastError=true)]
 static extern SafeFileHandle CreateFile(string path,uint access,uint share,IntPtr security,uint creation,uint flags,IntPtr template);
 [DllImport("kernel32.dll",SetLastError=true)]
 static extern bool DeviceIoControl(SafeFileHandle handle,uint code,byte[] input,uint inputSize,byte[] output,uint outputSize,out uint returned,IntPtr overlapped);
 static SafeFileHandle Open(uint index,uint access) {
  var handle=CreateFile(@"\\.\PhysicalDrive"+index,access,7,IntPtr.Zero,3,0,IntPtr.Zero);
  if(handle.IsInvalid) {int error=Marshal.GetLastWin32Error();handle.Dispose();throw new Win32Exception(error);}
  return handle;
 }
 static byte[] Query(SafeFileHandle handle,uint code,byte[] input,uint size) {
  var output=new byte[size];uint returned;
  if(!DeviceIoControl(handle,code,input,input==null?0:(uint)input.Length,output,size,out returned,IntPtr.Zero))
   throw new Win32Exception(Marshal.GetLastWin32Error());
  if(returned<size && code==0x700f0)throw new InvalidOperationException("Incomplete disk attributes");
  return output;
 }
 static string Serial(SafeFileHandle handle) {
  // IOCTL_STORAGE_QUERY_PROPERTY, StorageDeviceProperty / PropertyStandardQuery.
  var descriptor=Query(handle,0x2d1400,new byte[12],4096);
  var offset=BitConverter.ToUInt32(descriptor,24);
  if(offset==0 || offset>=descriptor.Length)return "";
  int end=(int)offset;
  while(end<descriptor.Length && descriptor[end]!=0)end++;
  return Encoding.ASCII.GetString(descriptor,(int)offset,end-(int)offset).Trim();
 }
 static ulong Attributes(SafeFileHandle handle) {
  return BitConverter.ToUInt64(Query(handle,0x700f0,null,16),8);
 }
 public static bool EnsureOnline(uint index,string expected) {
  using(var handle=Open(index,0)) {
   if(!String.Equals(Serial(handle),expected,StringComparison.OrdinalIgnoreCase))return false;
   if((Attributes(handle)&3)==0)return true;
  }
  using(var handle=Open(index,0xc0000000)) {
   if(!String.Equals(Serial(handle),expected,StringComparison.OrdinalIgnoreCase))return false;
   if((Attributes(handle)&3)==0)return true;
   // SET_DISK_ATTRIBUTES: persist only OFFLINE and READ_ONLY changes.
   // Other attributes, including storage-pool management, are preserved.
   var input=new byte[40];
   Array.Copy(BitConverter.GetBytes((uint)40),0,input,0,4);
   input[4]=1;
   Array.Copy(BitConverter.GetBytes((ulong)3),0,input,16,8);
   uint returned;
   if(!DeviceIoControl(handle,0x7c0f4,input,40,null,0,out returned,IntPtr.Zero))
    throw new Win32Exception(Marshal.GetLastWin32Error());
   if((Attributes(handle)&3)!=0)throw new InvalidOperationException("Converted disk is still offline or read-only");
   return true;
  }
 }
}
'@
$remaining=@(__MIGRATEKIT_VOLUME_SERIALS__)
for ($attempt=0; $attempt -lt 60 -and $remaining.Count; $attempt++) {
 $disks=@(Get-PhysicalDisk)
 $remaining=@(foreach ($id in $remaining) {
  $disk=@($disks | Where-Object { $_.SerialNumber -and $_.SerialNumber.Trim() -eq $id })
  if (!$disk.Count) { $id; continue }
  if ($disk.Count -ne 1) { throw 'Ambiguous converted disk serial' }
  if (![MigratekitConvertedDisk]::EnsureOnline([uint32]$disk[0].DeviceID,$id)) { $id }
 })
 if ($remaining.Count) { Start-Sleep -Seconds 2 }
}
if ($remaining.Count) { throw 'Converted volumes were not discovered during firstboot' }

# Windows Server 2022 Reboot Cycle Fix

## Issue Summary

Windows Server 2022 virtual machines migrated using migratekit were experiencing infinite reboot cycles after migration. The migration would complete successfully, but the VM would continuously reboot upon startup in the OpenStack environment.

### Related Issues
- Issue #136: Windows Server 2022 stuck in a reboot cycle post migration
- Issue #25: Windows VMs in Reboot Cycle after Migration (closed as expected behavior)

## Root Cause Analysis

### Investigation Timeline

1. **Last Working Image**: Users reported that Docker image `ghcr.io/vexxhost/migratekit@sha256:ead70cfc9efb3163990212f48e56cb347b109cb17a869a1d1c820b1c229700fe` worked correctly without reboot issues.

2. **Error Logs**: From the firstboot logs (`C:\Program Files\Guestfs\Firstboot\log.txt`), the error was:
   ```
   Installing: smbus.inf.
   Microsoft PnP Utility
   
   Processing inf :            smbus.inf
   Adding the driver package failed : A certificate chain processed, but terminated in a root certificate which is not trusted by the trust provider.
   
   Total attempted:              1
   Number successfully imported: 0
   
   Failed to install smbus.inf.
   .... exit code 249
   Script failed, will retry on next boot
   ```

3. **Breaking Change Identified**: 
   - Commit: `ccdfc8f` (September 13, 2025)
   - Change: Upgrade from Fedora 42 to Fedora 44
   - This upgrade included newer versions of:
     - `virtio-win` package (from ~0.1.248-1 to 0.1.285-1)
     - `virt-v2v` package
     - Associated dependencies

### Technical Explanation

The issue occurs because:

1. **Certificate Trust Chain**: Newer virtio-win drivers in Fedora 44 (version 0.1.285-1) contain drivers that are signed with certificates not fully trusted by Windows Server 2022's default certificate store.

2. **smbus.inf Driver**: The SMBus (System Management Bus) driver specifically fails to install due to the untrusted certificate chain, causing Windows to mark the firstboot script as failed.

3. **Reboot Loop**: When the firstboot script fails (exit code 249), it's scheduled to retry on the next boot. This creates an infinite loop:
   - Windows boots
   - Firstboot service runs
   - smbus.inf installation fails
   - System reboots to retry
   - Loop continues

4. **Why Some Reboots are Normal**: According to libguestfs documentation, Windows VMs converted with virt-v2v typically reboot 4+ times during first boot to install drivers and configure the system. However, this issue causes the reboot cycle to continue indefinitely.

## Solution

### Implemented Fix

Downgrade the base Fedora image from version 44 to version 42 in the Dockerfile. This ensures we use virtio-win driver packages (version ~0.1.248-1) that have properly trusted certificates for Windows Server 2022.

**Changed in**: `Dockerfile`
```dockerfile
# Before:
FROM fedora:44 AS build
...
FROM fedora:44

# After:
FROM fedora:42 AS build
...
FROM fedora:42
```

### Why This Works

Fedora 42's virtio-win package (version 0.1.248-1) contains drivers with certificate chains that are properly trusted by Windows Server 2022. The drivers are still digitally signed by Red Hat with Microsoft-recognized certificates, but the older version's certificate chain is fully compatible with Windows Server 2022's certificate store.

## Alternative Solutions Considered

1. **Manual Certificate Import**: Users could manually import the Red Hat root certificates into Windows before migration, but this is not practical for automated migrations.

2. **Disable Driver Signature Enforcement**: This is a security risk and not recommended for production environments.

3. **Pin virtio-win Package Version**: Instead of downgrading Fedora entirely, pin only the virtio-win package to an older version. This is more granular but requires more maintenance.

4. **Wait for virtio-win Update**: The virtio-win project may release an updated package with fixed certificates, but timeline is uncertain.

## Testing and Validation

To validate this fix:

1. Build the Docker image with Fedora 42
2. Perform a migration of a Windows Server 2022 VM
3. Observe the VM after cutover:
   - Should boot successfully
   - May reboot 2-5 times (normal behavior)
   - Should eventually stabilize and present login screen
   - Check `C:\Program Files\Guestfs\Firstboot\log.txt` for successful driver installation

## Workaround for Users (Before Fix)

Users experiencing this issue can use the older working image:
```bash
docker pull ghcr.io/vexxhost/migratekit@sha256:ead70cfc9efb3163990212f48e56cb347b109cb17a869a1d1c820b1c229700fe
```

## Future Considerations

1. **Monitor virtio-win Updates**: Track Fedora's virtio-win package updates and test new versions with Windows Server 2022 before upgrading.

2. **Automated Testing**: Implement CI/CD tests that validate Windows Server migrations don't result in reboot loops.

3. **Version Pinning Strategy**: Consider explicitly pinning the virtio-win package version in the Dockerfile to prevent unexpected breakage from package updates.

4. **Documentation Updates**: Update README.md to mention known Windows Server 2022 compatibility and any workarounds.

## References

- [virtio-win Package Changelog](https://fedorapeople.org/groups/virt/virtio-win/CHANGELOG)
- [virt-v2v Windows Guest Conversion Documentation](https://libguestfs.org/virt-v2v.1.html#converting-a-windows-guest)
- [Windows VirtIO Drivers - Proxmox Wiki](https://pve.proxmox.com/wiki/Windows_VirtIO_Drivers)
- Issue #136: https://github.com/vexxhost/migratekit/issues/136
- Issue #25: https://github.com/vexxhost/migratekit/issues/25

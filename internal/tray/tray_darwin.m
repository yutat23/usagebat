#import <Cocoa/Cocoa.h>

#include "tray_darwin.h"
#include "_cgo_export.h"

@interface UBDelegate : NSObject <NSApplicationDelegate>
@property(strong) NSStatusItem *item;
@property(strong) NSMenu *menu;
@end

static UBDelegate *gDelegate = nil;

@implementation UBDelegate

- (void)applicationDidFinishLaunching:(NSNotification *)note {
  self.item = [[NSStatusBar systemStatusBar]
      statusItemWithLength:NSVariableStatusItemLength];
  self.menu = [[NSMenu alloc] init];
  // Rows are enabled explicitly; AppKit's automatic enabling would grey out
  // every item because they have no responder-chain target.
  [self.menu setAutoenablesItems:NO];
  self.item.menu = self.menu;
  self.item.button.imagePosition = NSImageOnly;

  // The Go side owns all state. Polling from the main thread means every
  // AppKit mutation happens where AppKit requires it, with no cross-thread
  // dispatch and no partially applied updates.
  [NSTimer scheduledTimerWithTimeInterval:0.2
                                   target:self
                                 selector:@selector(tick:)
                                 userInfo:nil
                                  repeats:YES];
  ubReady();
}

- (void)tick:(NSTimer *)timer {
  ubTick();
}

- (void)clicked:(NSMenuItem *)sender {
  ubMenuClicked((int)sender.tag);
}

@end

void ubRun(void) {
  @autoreleasepool {
    [NSApplication sharedApplication];
    // Accessory: menu-bar presence only, no Dock tile and no main menu.
    [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
    gDelegate = [[UBDelegate alloc] init];
    [NSApp setDelegate:gDelegate];
    [NSApp run];
  }
}

static int ubReadDarkMode(void) {
  NSAppearance *appearance = gDelegate.item.button.effectiveAppearance;
  if (appearance == nil) {
    appearance = NSApp.effectiveAppearance;
  }
  NSString *match = [appearance bestMatchFromAppearancesWithNames:@[
    NSAppearanceNameAqua, NSAppearanceNameDarkAqua
  ]];
  return [match isEqualToString:NSAppearanceNameDarkAqua] ? 1 : 0;
}

int ubIsDarkMode(void) {
  if ([NSThread isMainThread]) {
    return ubReadDarkMode();
  }
  __block int dark = 0;
  dispatch_sync(dispatch_get_main_queue(), ^{
    dark = ubReadDarkMode();
  });
  return dark;
}

void ubSetIcon(const void *bytes, int len, double widthPt, double heightPt) {
  if (gDelegate == nil || bytes == NULL || len <= 0) {
    return;
  }
  @autoreleasepool {
    NSData *data = [NSData dataWithBytes:bytes length:(NSUInteger)len];
    NSImage *image = [[NSImage alloc] initWithData:data];
    if (image == nil) {
      return;
    }
    // The bitmap is rendered at an integer multiple of this logical size, so
    // setting the size here maps art dots onto backing pixels exactly.
    [image setSize:NSMakeSize(widthPt, heightPt)];
    image.template = NO;
    gDelegate.item.button.image = image;
    gDelegate.item.button.imagePosition = NSImageOnly;
  }
}

void ubSetTooltip(const char *s) {
  if (gDelegate == nil || s == NULL) {
    return;
  }
  gDelegate.item.button.toolTip = [NSString stringWithUTF8String:s];
}

void ubClearMenu(void) {
  if (gDelegate == nil) {
    return;
  }
  [gDelegate.menu removeAllItems];
}

void ubAddItem(const char *title, int tag, int enabled, int checked, int indent,
               int isSeparator) {
  if (gDelegate == nil) {
    return;
  }
  if (isSeparator) {
    [gDelegate.menu addItem:[NSMenuItem separatorItem]];
    return;
  }
  @autoreleasepool {
    NSString *text = title == NULL ? @"" : [NSString stringWithUTF8String:title];
    NSMenuItem *mi = [[NSMenuItem alloc] initWithTitle:text
                                               action:@selector(clicked:)
                                        keyEquivalent:@""];
    mi.target = gDelegate;
    mi.tag = tag;
    mi.enabled = enabled ? YES : NO;
    mi.state = checked ? NSControlStateValueOn : NSControlStateValueOff;
    mi.indentationLevel = indent;
    [gDelegate.menu addItem:mi];
  }
}

void ubQuit(void) {
  dispatch_async(dispatch_get_main_queue(), ^{
    [NSApp terminate:nil];
  });
}

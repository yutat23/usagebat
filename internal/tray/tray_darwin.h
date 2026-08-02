#ifndef USAGEBAT_TRAY_DARWIN_H
#define USAGEBAT_TRAY_DARWIN_H

// Status-item control surface. Every function must be called on the main
// thread; the Go side only calls them from within ubTick.
void ubRun(void);
void ubSetIcon(const void *bytes, int len, double widthPt, double heightPt);
void ubSetTooltip(const char *s);
void ubClearMenu(void);
void ubAddItem(const char *title, int tag, int enabled, int checked, int indent,
               int isSeparator);
void ubQuit(void);

#endif

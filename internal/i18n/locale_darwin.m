#import <Foundation/Foundation.h>
#include <stdlib.h>
#include <string.h>

char *usagebatPreferredLanguage(void) {
  NSString *language = [NSLocale preferredLanguages].firstObject;
  if (language == nil) return NULL;
  return strdup(language.UTF8String);
}

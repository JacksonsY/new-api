/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import i18n from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'

import {
  createLanguageBundleLoader,
  type LocaleModule,
} from './language-bundle-loader'
import {
  convertDetectedLanguage,
  normalizeInterfaceLanguage,
} from './languages'

// Each locale bundle is ~350–530 KB of JSON. Static-importing all seven put
// every language into the initial chunk (~2.5 MB raw, the bulk of the entry
// bundle), so every visitor downloaded six languages they will never see.
// Instead we code-split each locale (dynamic import → its own chunk) and load
// only the active language, plus English as the fallback, before first render.
// Keys must match the interface language codes (`zhCN` / `zhTW`, see
// languages.ts) that normalizeInterfaceLanguage / convertDetectedLanguage emit.
const localeLoaders: Record<string, () => Promise<LocaleModule>> = {
  en: () => import('./locales/en.json'),
  zhCN: () => import('./locales/zh.json'),
  fr: () => import('./locales/fr.json'),
  ru: () => import('./locales/ru.json'),
  ja: () => import('./locales/ja.json'),
  vi: () => import('./locales/vi.json'),
  zhTW: () => import('./locales/zh-TW.json'),
}

const loadLanguage = createLanguageBundleLoader(
  localeLoaders,
  (language, translation) => {
    i18n.addResourceBundle(language, 'translation', translation, true, true)
  }
)

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    // Languages are added on demand via addResourceBundle (see loadLanguage).
    resources: {},
    partialBundledLanguages: true,
    fallbackLng: 'en',
    supportedLngs: ['en', 'zhCN', 'fr', 'ru', 'ja', 'vi', 'zhTW'],
    load: 'currentOnly',
    nsSeparator: false, // Allow literal colons in keys (e.g., URLs, labels)
    debug: import.meta.env.DEV,
    interpolation: {
      escapeValue: false, // not needed for react as it escapes by default
    },
    detection: {
      order: ['localStorage', 'navigator'],
      caches: ['localStorage'],
      // Browsers report `zh-CN`/`zh-TW`/`zh`; map them onto our `zhCN`/`zhTW`
      // codes (non-Chinese codes pass through for normal supportedLngs matching).
      convertDetectedLanguage,
    },
    react: {
      // Locales are added on demand (addResourceBundle) after a language switch,
      // so components must re-render on the store's "added" event too — otherwise
      // switching to a not-yet-loaded language keeps showing the fallback text.
      bindI18nStore: 'added',
    },
  })

// Resolves once the active language (and the English fallback) are loaded.
// main.tsx awaits this before rendering so first paint is fully translated.
export const i18nReady: Promise<void> = Promise.all([
  loadLanguage('en'),
  loadLanguage(normalizeInterfaceLanguage(i18n.language)),
]).then(() => undefined)

// Load a language bundle the first time the user switches to it.
i18n.on('languageChanged', (lng) => {
  void loadLanguage(normalizeInterfaceLanguage(lng)).catch(() => undefined)
})

export default i18n

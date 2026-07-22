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
type LocaleBundle = { translation: Record<string, string> }

export type LocaleModule = {
  default?: LocaleBundle
  translation?: Record<string, string>
}

type LocaleLoaders = Record<string, () => Promise<LocaleModule>>
type AddResourceBundle = (
  language: string,
  translation: Record<string, string>
) => void

export function createLanguageBundleLoader(
  localeLoaders: LocaleLoaders,
  addResourceBundle: AddResourceBundle,
  fallbackLanguage = 'en'
): (language: string) => Promise<void> {
  const loadedOrInFlight = new Map<string, Promise<void>>()

  return (language) => {
    const code = localeLoaders[language] ? language : fallbackLanguage
    const existing = loadedOrInFlight.get(code)
    if (existing) return existing

    const task = localeLoaders[code]()
      .then((module) => {
        const translation = module.default?.translation ?? module.translation
        if (!translation) {
          throw new Error(`Locale bundle ${code} has no translations`)
        }
        addResourceBundle(code, translation)
      })
      .catch((error: unknown) => {
        loadedOrInFlight.delete(code)
        throw error
      })

    loadedOrInFlight.set(code, task)
    return task
  }
}

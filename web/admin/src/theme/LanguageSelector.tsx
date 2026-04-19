import { useI18n, type LocaleKey } from '../i18n'

export function LanguageSelector() {
  const { t, locale, setLocale } = useI18n()

  return (
    <select
      class="lang-select"
      value={locale}
      onChange={(e) => setLocale((e.currentTarget as HTMLSelectElement).value as LocaleKey)}
      aria-label={t('language.selectorLabel')}
    >
      <option value="zh">中文</option>
      <option value="en">EN</option>
      <option value="ja">日本語</option>
      <option value="ko">한국어</option>
      <option value="es">Español</option>
      <option value="fr">Français</option>
      <option value="de">Deutsch</option>
    </select>
  )
}

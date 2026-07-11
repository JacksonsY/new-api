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
import { useNavigate } from '@tanstack/react-router'
import { useCallback, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { PremiumPublicLayout } from '@/components/layout'
import { PageTransition } from '@/components/page-transition'

import {
  LoadingSkeleton,
  EmptyState,
  PricingTable,
  PricingToolbar,
  ModelCardGrid,
} from './components'
import { EXCLUDED_GROUPS, VIEW_MODES } from './constants'
import { useFilters } from './hooks/use-filters'
import { usePricingData } from './hooks/use-pricing-data'

export function Pricing() {
  const { t } = useTranslation()
  const navigate = useNavigate({ from: '/pricing/' })

  const {
    models,
    vendors,
    groupRatio,
    usableGroup,
    isLoading,
    priceRate,
    usdExchangeRate,
  } = usePricingData()

  const {
    searchInput,
    sortBy,
    vendorFilter,
    groupFilter,
    quotaTypeFilter,
    endpointTypeFilter,
    tagFilter,
    tokenUnit,
    viewMode,
    showRechargePrice,
    setSearchInput,
    setSortBy,
    setVendorFilter,
    setGroupFilter,
    setQuotaTypeFilter,
    setEndpointTypeFilter,
    setTagFilter,
    setTokenUnit,
    setViewMode,
    setShowRechargePrice,
    filteredModels,
    hasActiveFilters,
    activeFilterCount,
    availableTags,
    routeSearch,
    clearFilters,
    clearSearch,
  } = useFilters(models || [])

  const handleModelClick = useCallback(
    (modelName: string) => {
      navigate({
        to: '/pricing/$modelId',
        params: { modelId: modelName },
        search: routeSearch,
      })
    },
    [navigate, routeSearch]
  )

  const availableGroups = useMemo(
    () =>
      Object.keys(usableGroup || {}).filter(
        (g) => !EXCLUDED_GROUPS.includes(g)
      ),
    [usableGroup]
  )

  const handleClearAll = useCallback(() => {
    clearFilters()
    clearSearch()
  }, [clearFilters, clearSearch])

  let pricingContent = (
    <PricingTable
      models={filteredModels}
      priceRate={priceRate}
      usdExchangeRate={usdExchangeRate}
      tokenUnit={tokenUnit}
      showRechargePrice={showRechargePrice}
      selectedGroup={groupFilter}
      onModelClick={handleModelClick}
    />
  )

  if (filteredModels.length === 0) {
    pricingContent = (
      <EmptyState
        searchQuery={searchInput}
        hasActiveFilters={hasActiveFilters}
        onClearFilters={handleClearAll}
      />
    )
  } else if (viewMode === VIEW_MODES.CARD) {
    pricingContent = (
      <ModelCardGrid
        models={filteredModels}
        onModelClick={handleModelClick}
        priceRate={priceRate}
        usdExchangeRate={usdExchangeRate}
        tokenUnit={tokenUnit}
        showRechargePrice={showRechargePrice}
        selectedGroup={groupFilter}
      />
    )
  }

  if (isLoading) {
    return (
      <PremiumPublicLayout showMainContainer={false}>
        <div className='mx-auto w-full max-w-[1800px] px-3 pt-24 pb-8 sm:px-6 sm:pt-28 sm:pb-10 xl:px-8'>
          <LoadingSkeleton viewMode={viewMode} />
        </div>
      </PremiumPublicLayout>
    )
  }

  return (
    <PremiumPublicLayout showMainContainer={false}>
      <div className='relative'>
        <PageTransition className='relative mx-auto w-full max-w-[1800px] px-3 pt-24 pb-8 sm:px-6 sm:pt-28 sm:pb-10 xl:px-8'>
          {/* 搜索已并入下方 PricingToolbar（上游重构），品牌 hero 只保留文案。 */}
          <header className='mx-auto mb-6 max-w-3xl pt-4 text-center sm:mb-10 sm:pt-8'>
            <p className='pf-eyebrow justify-center'>{t('Model Square')}</p>
            <h1 className='pf-display mt-3 !text-[clamp(2.2rem,5.5vw,3.6rem)]'>
              {t('Every model,')}{' '}
              <span className='pf-fire-text'>{t('one gateway.')}</span>
            </h1>
            <p className='text-muted-foreground/80 mt-4 text-sm sm:text-base'>
              {t('This site currently has {{count}} models enabled', {
                count: models?.length || 0,
              })}
            </p>
            <p className='pf-lead mx-auto mt-2 max-w-2xl !text-[0.92rem]'>
              {t(
                'Discover curated AI models, compare pricing and capabilities, and choose the right model for every scenario.'
              )}
            </p>
          </header>

          <main className='space-y-4'>
            <PricingToolbar
              searchInput={searchInput}
              onSearchChange={setSearchInput}
              onClearSearch={clearSearch}
              filteredCount={filteredModels.length}
              totalCount={models?.length}
              sortBy={sortBy}
              onSortChange={setSortBy}
              tokenUnit={tokenUnit}
              onTokenUnitChange={setTokenUnit}
              showRechargePrice={showRechargePrice}
              onRechargePriceChange={setShowRechargePrice}
              viewMode={viewMode}
              onViewModeChange={setViewMode}
              quotaTypeFilter={quotaTypeFilter}
              endpointTypeFilter={endpointTypeFilter}
              vendorFilter={vendorFilter}
              groupFilter={groupFilter}
              tagFilter={tagFilter}
              onQuotaTypeChange={setQuotaTypeFilter}
              onEndpointTypeChange={setEndpointTypeFilter}
              onVendorChange={setVendorFilter}
              onGroupChange={setGroupFilter}
              onTagChange={setTagFilter}
              vendors={vendors || []}
              groups={availableGroups}
              groupRatios={groupRatio}
              tags={availableTags}
              models={models || []}
              hasActiveFilters={hasActiveFilters}
              activeFilterCount={activeFilterCount}
              onClearFilters={clearFilters}
            />

            {pricingContent}
          </main>
        </PageTransition>
      </div>
    </PremiumPublicLayout>
  )
}

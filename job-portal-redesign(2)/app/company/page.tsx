'use client'

import { useState, useEffect, useRef, Suspense } from 'react'
import Link from 'next/link'
import { usePathname, useSearchParams } from 'next/navigation'
import QueueSidebar from '@/components/job-hunt/queue-sidebar'

interface Job {
  title: string
  location: string
  locationTag: string
  url: string
  isIndia: boolean
}

interface CompanyData {
  name: string
  isIndian: boolean
  jobs: Job[]
  status?: string
}

function CompanyContent() {
  const pathname = usePathname()
  const searchParams = useSearchParams()
  const companyName = decodeURIComponent(pathname.split('/').pop() || '')
  const searchQuery = searchParams.get('search') || ''
  
  const [company, setCompany] = useState<CompanyData | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [renderPage, setRenderPage] = useState(0)
  const [applyQueue, setApplyQueue] = useState(new Map())
  const scrollTriggerRef = useRef<HTMLDivElement>(null)

  const BATCH_SIZE = 40

  // Fetch company data
  useEffect(() => {
    const fetchData = async () => {
      setIsLoading(true)
      try {
        const response = await fetch(
          `/api/company?name=${encodeURIComponent(companyName)}&search=${encodeURIComponent(searchQuery)}`
        )
        const data = await response.json()
        setCompany(data)
      } catch (error) {
        console.error('Failed to fetch company:', error)
      } finally {
        setIsLoading(false)
      }
    }

    fetchData()
  }, [companyName, searchQuery])

  // Add to queue
  const addToQueue = (job: Job) => {
    if (applyQueue.has(job.url)) return
    
    const newQueue = new Map(applyQueue)
    newQueue.set(job.url, {
      ...job,
      company: company?.name || companyName,
    })
    
    setApplyQueue(newQueue)
  }

  // Remove from queue
  const removeFromQueue = (url: string) => {
    const newQueue = new Map(applyQueue)
    newQueue.delete(url)
    setApplyQueue(newQueue)
  }

  // Get rendered jobs
  const renderedJobs = company?.jobs.slice(0, (renderPage + 1) * BATCH_SIZE) || []

  // Intersection Observer for infinite scroll
  useEffect(() => {
    if (!scrollTriggerRef.current || !company?.jobs) return

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && company.jobs.length > ((renderPage + 1) * BATCH_SIZE)) {
          setRenderPage(prev => prev + 1)
        }
      },
      { rootMargin: '200px' }
    )

    observer.observe(scrollTriggerRef.current)
    return () => observer.disconnect()
  }, [renderPage, company?.jobs])

  return (
    <div className="min-h-screen bg-gray-50">
      {/* Header */}
      <nav className="bg-white border-b border-gray-200 sticky top-0 z-10">
        <div className="max-w-4xl mx-auto px-4 py-4 flex justify-between items-center">
          <Link href="/" className="text-2xl font-bold text-gray-900">
            Job Hunt
          </Link>
          <a 
            href="#" 
            className="px-4 py-2 text-sm font-medium text-gray-700 bg-gray-100 rounded-lg hover:bg-gray-200 transition-colors"
          >
            Sign In
          </a>
        </div>
      </nav>

      {/* Main Content */}
      <div className="max-w-4xl mx-auto py-8 px-4">
        {/* Back Button */}
        <Link
          href={searchQuery ? `/?search=${encodeURIComponent(searchQuery)}` : '/'}
          className="inline-flex items-center gap-2 px-6 py-3 bg-white border border-gray-300 rounded-lg shadow-sm hover:bg-gray-50 transition-colors font-medium text-gray-700 mb-8"
        >
          <span>⬅️</span> Back to Search
        </Link>

        {/* Company Header */}
        {isLoading ? (
          <div className="flex items-center justify-center py-20">
            <div className="font-mono text-gray-600 font-medium tracking-wide">[ 🔃 SYNCING TITAN INDEX... ]</div>
          </div>
        ) : company ? (
          <>
            <div className="mb-12 flex items-center gap-6">
              <div className="w-20 h-20 bg-gradient-to-br from-blue-500 to-blue-600 rounded-lg flex items-center justify-center text-white font-bold text-4xl shadow-md flex-shrink-0">
                {company.name.charAt(0).toUpperCase()}
              </div>
              <div>
                <h1 className="text-4xl md:text-5xl font-bold text-gray-900 capitalize mb-2">
                  {company.name}
                </h1>
                <span className={`text-sm font-bold tracking-widest uppercase ${
                  company.isIndian ? 'text-green-600' : 'text-blue-600'
                }`}>
                  {company.isIndian ? '🇮🇳 India HQ' : '🌍 Global Corp'}
                </span>
              </div>
            </div>

            {/* Jobs List */}
            {company.status === 'no_jobs' || !company.jobs || company.jobs.length === 0 ? (
              <div className="text-center py-20">
                <h3 className="text-2xl font-bold text-gray-700 mb-2">No active positions</h3>
                <p className="text-lg text-gray-500">
                  {searchQuery
                    ? `No positions match "${searchQuery}"`
                    : 'This company currently has no open positions'}
                </p>
              </div>
            ) : (
              <>
                <div className="space-y-4 mb-8">
                  {renderedJobs.map((job) => {
                    const isQueued = applyQueue.has(job.url)
                    return (
                      <div
                        key={job.url}
                        className="bg-white rounded-lg shadow-sm border border-gray-200 p-6 hover:shadow-md transition-shadow"
                      >
                        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                          <div className="flex-1">
                            <h3 className="text-xl font-bold text-gray-900 mb-2">
                              {job.title}
                            </h3>
                            <div className="flex items-center gap-3">
                              <span className={`text-xs font-bold px-2 py-0.5 rounded border ${
                                job.isIndia
                                  ? 'bg-green-100 text-green-700 border-green-200'
                                  : 'bg-gray-100 text-gray-700 border-gray-200'
                              }`}>
                                {job.locationTag}
                              </span>
                              <span className="text-sm text-gray-600 font-medium">
                                {job.location}
                              </span>
                            </div>
                          </div>
                          <div className="flex flex-col sm:flex-row gap-3">
                            <button
                              onClick={() => addToQueue(job)}
                              disabled={isQueued}
                              className={`px-4 py-2.5 text-sm font-bold rounded-lg shadow-sm transition border ${
                                isQueued
                                  ? 'bg-green-50 text-green-700 border-green-200 cursor-default'
                                  : 'bg-blue-50 text-blue-700 border-blue-200 hover:bg-blue-100'
                              }`}
                            >
                              {isQueued ? '✓ Added to Queue' : '➕ Add to Queue'}
                            </button>
                            <a
                              href={job.url}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="px-6 py-2.5 bg-gray-900 text-white text-sm font-bold rounded-lg hover:bg-gray-800 transition-colors text-center"
                            >
                              Direct Apply ↗
                            </a>
                          </div>
                        </div>
                      </div>
                    )
                  })}
                </div>

                {/* Infinite Scroll Trigger */}
                <div ref={scrollTriggerRef} className="h-20 flex items-center justify-center">
                  {company.jobs.length > renderedJobs.length && (
                    <div className="text-gray-400">Loading more jobs...</div>
                  )}
                </div>
              </>
            )}
          </>
        ) : (
          <div className="text-center py-20">
            <h3 className="text-2xl font-bold text-gray-700">Company not found</h3>
          </div>
        )}
      </div>

      {/* Queue Sidebar */}
      <QueueSidebar
        applyQueue={applyQueue}
        onRemoveFromQueue={removeFromQueue}
      />
    </div>
  )
}

export default function CompanyPage() {
  return (
    <Suspense fallback={
      <div className="flex items-center justify-center min-h-screen">
        <div className="font-mono text-gray-600 font-medium tracking-wide">[ 🔃 SYNCING TITAN INDEX... ]</div>
      </div>
    }>
      <CompanyContent />
    </Suspense>
  )
}

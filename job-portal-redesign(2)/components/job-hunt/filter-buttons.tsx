interface FilterButtonsProps {
  onSetQuery: (query: string) => void
}

export default function FilterButtons({
  onSetQuery,
}: FilterButtonsProps) {
  return (
    <div className="flex flex-wrap justify-center gap-3 mb-12">
      <button
        onClick={() => onSetQuery('AI')}
        className="px-4 py-2 bg-white border border-gray-300 text-gray-700 rounded-lg font-medium hover:bg-gray-50 transition-colors"
      >
        🤖 AI
      </button>
      <button
        onClick={() => onSetQuery('ML')}
        className="px-4 py-2 bg-white border border-gray-300 text-gray-700 rounded-lg font-medium hover:bg-gray-50 transition-colors"
      >
        🧠 ML
      </button>
      <button
        onClick={() => onSetQuery('Full stack')}
        className="px-4 py-2 bg-white border border-gray-300 text-gray-700 rounded-lg font-medium hover:bg-gray-50 transition-colors"
      >
        💻 Full Stack
      </button>
    </div>
  )
}
